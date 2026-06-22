package controller

import (
	"context"
	"fmt"

	"helm.sh/helm/v3/pkg/action"
	"helm.sh/helm/v3/pkg/chart/loader"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	fleetv1alpha1 "github.com/container-registry/harbor-satellite/operator/api/v1alpha1"
)

const finalizerName = "fleet.harbor-satellite.io/gc-deregister"

// SatelliteReconciler reconciles Satellite objects against Ground Control.
type SatelliteReconciler struct {
	client.Client
	HelmDriver string
}

func (r *SatelliteReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	var sat fleetv1alpha1.Satellite
	if err := r.Get(ctx, req.NamespacedName, &sat); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	if !sat.DeletionTimestamp.IsZero() {
		return r.reconcileDelete(ctx, &sat)
	}

	if !controllerutil.ContainsFinalizer(&sat, finalizerName) {
		controllerutil.AddFinalizer(&sat, finalizerName)
		if err := r.Update(ctx, &sat); err != nil {
			return ctrl.Result{}, fmt.Errorf("add finalizer: %w", err)
		}
		return ctrl.Result{}, nil
	}

	if err := r.reconcileHelmRelease(ctx, &sat); err != nil {
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

func (r *SatelliteReconciler) reconcileDelete(ctx context.Context, sat *fleetv1alpha1.Satellite) (ctrl.Result, error) {
	if err := r.uninstallRelease(sat); err != nil {
		return ctrl.Result{}, err
	}
	controllerutil.RemoveFinalizer(sat, finalizerName)
	if err := r.Update(ctx, sat); err != nil {
		return ctrl.Result{}, fmt.Errorf("remove finalizer: %w", err)
	}
	return ctrl.Result{}, nil
}

func (r *SatelliteReconciler) reconcileHelmRelease(_ context.Context, sat *fleetv1alpha1.Satellite) error {
	cfg := new(action.Configuration)
	if err := cfg.Init(nil, sat.Namespace, r.HelmDriver, func(format string, v ...any) {
		ctrl.Log.Info(fmt.Sprintf(format, v...))
	}); err != nil {
		return fmt.Errorf("init helm config: %w", err)
	}

	releaseName := sat.Name
	histClient := action.NewHistory(cfg)
	histClient.Max = 1
	if _, err := histClient.Run(releaseName); err != nil {
		return r.installRelease(cfg, sat)
	}
	return r.upgradeRelease(cfg, sat)
}

func (r *SatelliteReconciler) installRelease(cfg *action.Configuration, sat *fleetv1alpha1.Satellite) error {
	install := action.NewInstall(cfg)
	install.ReleaseName = sat.Name
	install.Namespace = sat.Namespace
	install.CreateNamespace = true

	chartRef := ""
	if sat.Spec.GroundControlURL != "" {
		chartRef = "deploy/helm/satellite"
	}

	ch, err := loader.Load(chartRef)
	if err != nil {
		return fmt.Errorf("load chart: %w", err)
	}

	if _, err = install.Run(ch, nil); err != nil {
		return fmt.Errorf("helm install: %w", err)
	}
	return nil
}

func (r *SatelliteReconciler) upgradeRelease(cfg *action.Configuration, sat *fleetv1alpha1.Satellite) error {
	upgrade := action.NewUpgrade(cfg)
	upgrade.Namespace = sat.Namespace

	ch, err := loader.Load("deploy/helm/satellite")
	if err != nil {
		return fmt.Errorf("load chart: %w", err)
	}

	if _, err = upgrade.Run(sat.Name, ch, nil); err != nil {
		return fmt.Errorf("helm upgrade: %w", err)
	}
	return nil
}

func (r *SatelliteReconciler) uninstallRelease(sat *fleetv1alpha1.Satellite) error {
	cfg := new(action.Configuration)
	if err := cfg.Init(nil, sat.Namespace, r.HelmDriver, func(format string, v ...any) {
		ctrl.Log.Info(fmt.Sprintf(format, v...))
	}); err != nil {
		return fmt.Errorf("init helm config: %w", err)
	}

	uninstall := action.NewUninstall(cfg)
	if _, err := uninstall.Run(sat.Name); err != nil {
		return fmt.Errorf("helm uninstall: %w", err)
	}
	return nil
}

func (r *SatelliteReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&fleetv1alpha1.Satellite{}).
		Complete(r)
}
