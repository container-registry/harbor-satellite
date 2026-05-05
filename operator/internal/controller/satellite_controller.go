package controller

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

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

	// GroundControlURL is the default Ground Control server URL.
	GroundControlURL string

	httpClient *http.Client
}

// registerSatelliteRequest is the payload for POST /api/satellites.
type registerSatelliteRequest struct {
	Name       string    `json:"name"`
	Groups     *[]string `json:"groups,omitempty"`
	ConfigName string    `json:"config_name"`
}

// registerSatelliteResponse is the response from POST /api/satellites.
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

	gcURL := satellite.Spec.GroundControlURL
	if gcURL == "" {
		gcURL = r.GroundControlURL
	}

	if r.httpClient == nil {
		r.httpClient = &http.Client{Timeout: 30 * time.Second}
	}

	// Handle deletion: call Ground Control to deregister.
	if !satellite.DeletionTimestamp.IsZero() {
		if err := r.handleDelete(ctx, gcURL, satellite.Name); err != nil {
			logger.Error(err, "Failed to delete satellite from Ground Control")
			return ctrl.Result{}, err
		}
		return ctrl.Result{}, nil
	}

	// If already registered, skip.
	if satellite.Status.Registered {
		return ctrl.Result{}, nil
	}

	// Register with Ground Control.
	token, err := r.registerWithGroundControl(ctx, gcURL, satellite.Name, satellite.Spec.Group, satellite.Spec.ConfigName)
	if err != nil {
		satellite.Status.Phase = harborv1alpha1.SatellitePhaseFailed
		satellite.Status.Message = fmt.Sprintf("Registration failed: %v", err)
		satellite.Status.Conditions = append(satellite.Status.Conditions, metav1.Condition{
			Type:               "Registered",
			Status:             metav1.ConditionFalse,
			Reason:             "RegistrationFailed",
			Message:            err.Error(),
			LastTransitionTime: metav1.Now(),
		})
		if updateErr := r.Status().Update(ctx, &satellite); updateErr != nil {
			logger.Error(updateErr, "Failed to update satellite status")
		}
		return ctrl.Result{}, err
	}

	// Update status to Registered.
	satellite.Status.Phase = harborv1alpha1.SatellitePhaseRegistered
	satellite.Status.Registered = true
	satellite.Status.TokenRef = token
	satellite.Status.Message = "Successfully registered with Ground Control"
	satellite.Status.Conditions = append(satellite.Status.Conditions, metav1.Condition{
		Type:               "Registered",
		Status:             metav1.ConditionTrue,
		Reason:             "RegistrationSucceeded",
		Message:            "Satellite registered with Ground Control",
		LastTransitionTime: metav1.Now(),
	})

	if err := r.Status().Update(ctx, &satellite); err != nil {
		logger.Error(err, "Failed to update satellite status")
		return ctrl.Result{}, err
	}

	logger.Info("Satellite registered successfully", "name", satellite.Name, "phase", satellite.Status.Phase)
	return ctrl.Result{}, nil
}

// registerWithGroundControl creates a satellite via the Ground Control REST API.
func (r *SatelliteReconciler) registerWithGroundControl(ctx context.Context, gcURL, name, group, configName string) (string, error) {
	groups := &[]string{}
	if group != "" {
		*groups = append(*groups, group)
	}

	reqBody := registerSatelliteRequest{
		Name:       name,
		Groups:     groups,
		ConfigName: configName,
	}

	body, err := json.Marshal(reqBody)
	if err != nil {
		return "", fmt.Errorf("marshal request: %w", err)
	}

	url := gcURL + "/api/satellites"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := r.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("http request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
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
		return "", fmt.Errorf("API error (status %d): %s", resp.StatusCode, msg)
	}

	var regResp registerSatelliteResponse
	if err := json.NewDecoder(resp.Body).Decode(&regResp); err != nil {
		return "", fmt.Errorf("decode response: %w", err)
	}

	return regResp.Token, nil
}

// handleDelete calls DELETE /api/satellites/{name} on Ground Control.
func (r *SatelliteReconciler) handleDelete(ctx context.Context, gcURL, name string) error {
	url := gcURL + "/api/satellites/" + name
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, url, nil)
	if err != nil {
		return fmt.Errorf("create delete request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := r.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("http delete request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("API error (status %d)", resp.StatusCode)
	}

	return nil
}

// SetupWithManager sets up the controller with the manager.
func (r *SatelliteReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&harborv1alpha1.Satellite{}).
		Complete(r)
}
