package controller

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/go-logr/logr"
	harborv1alpha1 "github.com/container-registry/harbor-satellite/operator/api/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

// SatelliteReconciler reconciles Satellite objects against the Ground Control API.
type SatelliteReconciler struct {
	client.Client
	Scheme *runtime.Scheme

	GroundControlURL string

	httpClient *http.Client
}

type registerSatelliteRequest struct {
	Name       string    `json:"name"`
	Groups     *[]string `json:"groups,omitempty"`
	ConfigName string    `json:"config_name"`
}

type registerSatelliteResponse struct {
	Token string `json:"token"`
}

// Reconcile is the main reconciliation loop.
func (r *SatelliteReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	var satellite harborv1alpha1.Satellite
	if err := r.Get(ctx, req.NamespacedName, &satellite); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	gcURL := r.resolveGCURL(&satellite)
	r.ensureHTTPClient()

	if !satellite.DeletionTimestamp.IsZero() {
		return r.handleDeletion(ctx, logger, gcURL, satellite.Name)
	}

	if satellite.Status.Registered {
		return ctrl.Result{}, nil
	}

	return r.registerSatellite(ctx, logger, gcURL, &satellite)
}

func (r *SatelliteReconciler) resolveGCURL(sat *harborv1alpha1.Satellite) string {
	if sat.Spec.GroundControlURL != "" {
		return sat.Spec.GroundControlURL
	}
	return r.GroundControlURL
}

func (r *SatelliteReconciler) ensureHTTPClient() {
	if r.httpClient == nil {
		r.httpClient = &http.Client{Timeout: 30 * time.Second}
	}
}

func (r *SatelliteReconciler) handleDeletion(ctx context.Context, logger logr.Logger, gcURL, name string) (ctrl.Result, error) {
	if err := r.deleteFromGroundControl(ctx, gcURL, name); err != nil {
		logger.Error(err, "Failed to delete satellite from Ground Control")
		return ctrl.Result{}, err
	}
	return ctrl.Result{}, nil
}

func (r *SatelliteReconciler) registerSatellite(ctx context.Context, logger logr.Logger, gcURL string, sat *harborv1alpha1.Satellite) (ctrl.Result, error) {
	token, err := r.registerWithGroundControl(ctx, gcURL, sat.Name, sat.Spec.Group, sat.Spec.ConfigName)
	if err != nil {
		r.updateFailedStatus(ctx, sat, err)
		logger.Error(err, "Failed to register satellite")
		return ctrl.Result{}, err
	}

	r.updateRegisteredStatus(ctx, sat, token)
	logger.Info("Satellite registered successfully", "name", sat.Name, "phase", sat.Status.Phase)
	return ctrl.Result{}, nil
}

func (r *SatelliteReconciler) updateFailedStatus(ctx context.Context, sat *harborv1alpha1.Satellite, err error) {
	sat.Status.Phase = harborv1alpha1.SatellitePhaseFailed
	sat.Status.Message = fmt.Sprintf("Registration failed: %v", err)
	sat.Status.Conditions = append(sat.Status.Conditions, metav1.Condition{
		Type:               "Registered",
		Status:             metav1.ConditionFalse,
		Reason:             "RegistrationFailed",
		Message:            err.Error(),
		LastTransitionTime: metav1.Now(),
	})
	_ = r.Status().Update(ctx, sat)
}

func (r *SatelliteReconciler) updateRegisteredStatus(ctx context.Context, sat *harborv1alpha1.Satellite, token string) {
	sat.Status.Phase = harborv1alpha1.SatellitePhaseRegistered
	sat.Status.Registered = true
	sat.Status.TokenRef = token
	sat.Status.Message = "Successfully registered with Ground Control"
	sat.Status.Conditions = append(sat.Status.Conditions, metav1.Condition{
		Type:               "Registered",
		Status:             metav1.ConditionTrue,
		Reason:             "RegistrationSucceeded",
		Message:            "Satellite registered with Ground Control",
		LastTransitionTime: metav1.Now(),
	})
	_ = r.Status().Update(ctx, sat)
}

func (r *SatelliteReconciler) registerWithGroundControl(ctx context.Context, gcURL, name, group, configName string) (string, error) {
	groups := buildGroupsPtr(group)

	body, err := json.Marshal(registerSatelliteRequest{
		Name:       name,
		Groups:     groups,
		ConfigName: configName,
	})
	if err != nil {
		return "", fmt.Errorf("marshal request: %w", err)
	}

	resp, err := r.sendRequest(ctx, http.MethodPost, gcURL+"/api/satellites", body)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", parseAPIError(resp)
	}

	var regResp registerSatelliteResponse
	if err := json.NewDecoder(resp.Body).Decode(&regResp); err != nil {
		return "", fmt.Errorf("decode response: %w", err)
	}

	return regResp.Token, nil
}

func (r *SatelliteReconciler) deleteFromGroundControl(ctx context.Context, gcURL, name string) error {
	resp, err := r.sendRequest(ctx, http.MethodDelete, gcURL+"/api/satellites/"+name, nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return parseAPIError(resp)
	}

	return nil
}

func (r *SatelliteReconciler) sendRequest(ctx context.Context, method, url string, body []byte) (*http.Response, error) {
	var bodyReader *bytes.Reader
	if body != nil {
		bodyReader = bytes.NewReader(body)
	}

	req, err := http.NewRequestWithContext(ctx, method, url, bodyReader)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := r.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("http request: %w", err)
	}

	return resp, nil
}

func buildGroupsPtr(group string) *[]string {
	if group == "" {
		return &[]string{}
	}
	return &[]string{group}
}

func parseAPIError(resp *http.Response) error {
	var errResp struct {
		Error   string `json:"error"`
		Message string `json:"message"`
	}
	json.NewDecoder(resp.Body).Decode(&errResp)

	msg := errResp.Error
	if msg == "" {
		msg = errResp.Message
	}
	if msg == "" {
		msg = resp.Status
	}
	return fmt.Errorf("API error (status %d): %s", resp.StatusCode, msg)
}

func (r *SatelliteReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&harborv1alpha1.Satellite{}).
		Complete(r)
}
