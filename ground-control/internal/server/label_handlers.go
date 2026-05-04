package server

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"net/http"
	"regexp"

	"github.com/container-registry/harbor-satellite/ground-control/internal/database"
	"github.com/gorilla/mux"
)

type patchLabelsRequest map[string]*string

func (s *Server) getLabelsHandler(w http.ResponseWriter, r *http.Request) {
	sat, ok := s.resolveSatellite(w, r)
	if !ok {
		return
	}
	labels, err := s.dbQueries.GetLabelsBySatelliteID(r.Context(), sat.ID)
	if err != nil {
		log.Printf("Error: Failed to get labels for satellite %s: %v", sat.Name, err)
		HandleAppError(w, &AppError{Message: "failed to get labels", Code: http.StatusInternalServerError})
		return
	}
	WriteJSONResponse(w, http.StatusOK, labels)
}

func (s *Server) setLabelsHandler(w http.ResponseWriter, r *http.Request) {
	sat, ok := s.resolveSatellite(w, r)
	if !ok {
		return
	}
	var labels map[string]string
	if err := DecodeRequestBody(r, &labels); err != nil {
		HandleAppError(w, err)
		return
	}
	if appErr := validateLabels(labels); appErr != nil {
		HandleAppError(w, appErr)
		return
	}
	if err := s.replaceLabels(r.Context(), sat.ID, labels); err != nil {
		log.Printf("Error: Failed to set labels for satellite %s: %v", sat.Name, err)
		HandleAppError(w, &AppError{Message: "failed to set labels", Code: http.StatusInternalServerError})
		return
	}
	WriteJSONResponse(w, http.StatusOK, labels)
}

func (s *Server) patchLabelsHandler(w http.ResponseWriter, r *http.Request) {
	sat, ok := s.resolveSatellite(w, r)
	if !ok {
		return
	}
	var patch patchLabelsRequest
	if err := DecodeRequestBody(r, &patch); err != nil {
		HandleAppError(w, err)
		return
	}
	if appErr := validatePatch(patch); appErr != nil {
		HandleAppError(w, appErr)
		return
	}
	if err := s.applyLabelPatch(r.Context(), sat.ID, map[string]*string(patch)); err != nil {
		log.Printf("Error: Failed to patch labels for satellite %s: %v", sat.Name, err)
		HandleAppError(w, &AppError{Message: "failed to patch labels", Code: http.StatusInternalServerError})
		return
	}
	labels, err := s.dbQueries.GetLabelsBySatelliteID(r.Context(), sat.ID)
	if err != nil {
		log.Printf("Error: Failed to get labels after patch for satellite %s: %v", sat.Name, err)
		HandleAppError(w, &AppError{Message: "failed to get labels", Code: http.StatusInternalServerError})
		return
	}
	WriteJSONResponse(w, http.StatusOK, labels)
}

// replaceLabels atomically replaces all labels for a satellite.
func (s *Server) replaceLabels(ctx context.Context, satelliteID int32, labels map[string]string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	if err := s.runReplaceLabels(ctx, tx, satelliteID, labels); err != nil {
		return errors.Join(err, tx.Rollback())
	}
	return tx.Commit()
}

func (s *Server) runReplaceLabels(ctx context.Context, tx *sql.Tx, satelliteID int32, labels map[string]string) error {
	txQ := s.dbQueries.WithTx(tx)
	if err := txQ.DeleteLabelsBySatelliteID(ctx, satelliteID); err != nil {
		return err
	}
	for k, v := range labels {
		if err := txQ.UpsertLabel(ctx, satelliteID, k, v); err != nil {
			return err
		}
	}
	return nil
}

// applyLabelPatch atomically applies a patch: nil value removes a key, non-nil upserts.
func (s *Server) applyLabelPatch(ctx context.Context, satelliteID int32, patch map[string]*string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	if err := s.runPatchLabels(ctx, tx, satelliteID, patch); err != nil {
		return errors.Join(err, tx.Rollback())
	}
	return tx.Commit()
}

func (s *Server) runPatchLabels(ctx context.Context, tx *sql.Tx, satelliteID int32, patch map[string]*string) error {
	txQ := s.dbQueries.WithTx(tx)
	for k, v := range patch {
		if v == nil {
			if err := txQ.DeleteLabel(ctx, satelliteID, k); err != nil {
				return err
			}
			continue
		}
		if err := txQ.UpsertLabel(ctx, satelliteID, k, *v); err != nil {
			return err
		}
	}
	return nil
}

// resolveSatellite looks up a satellite by the {satellite} mux var.
func (s *Server) resolveSatellite(w http.ResponseWriter, r *http.Request) (database.Satellite, bool) {
	name := mux.Vars(r)["satellite"]
	sat, err := s.dbQueries.GetSatelliteByName(r.Context(), name)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			HandleAppError(w, &AppError{Message: "satellite not found", Code: http.StatusNotFound})
		} else {
			log.Printf("Error: Failed to get satellite %s: %v", name, err)
			HandleAppError(w, &AppError{Message: "failed to get satellite", Code: http.StatusInternalServerError})
		}
		return database.Satellite{}, false
	}
	return sat, true
}

func validateLabels(labels map[string]string) *AppError {
	for k, v := range labels {
		if err := validateLabelKey(k); err != nil {
			return &AppError{Message: err.Error(), Code: http.StatusBadRequest}
		}
		if err := validateLabelValue(v); err != nil {
			return &AppError{Message: err.Error(), Code: http.StatusBadRequest}
		}
	}
	return nil
}

func validatePatch(patch patchLabelsRequest) *AppError {
	for k, v := range patch {
		if err := validateLabelKey(k); err != nil {
			return &AppError{Message: err.Error(), Code: http.StatusBadRequest}
		}
		if v != nil {
			if err := validateLabelValue(*v); err != nil {
				return &AppError{Message: err.Error(), Code: http.StatusBadRequest}
			}
		}
	}
	return nil
}

func validateLabelKey(k string) error {
	if k == "" || len(k) > 316 {
		return fmt.Errorf("label key %q: must be 1–316 characters", k)
	}
	matched, _ := regexp.MatchString(`^[a-zA-Z0-9][a-zA-Z0-9._\-/]*$`, k)
	if !matched {
		return fmt.Errorf("label key %q: only alphanumeric, '.', '_', '-', '/' allowed", k)
	}
	return nil
}

func validateLabelValue(v string) error {
	if len(v) > 63 {
		return fmt.Errorf("label value exceeds 63 characters")
	}
	return nil
}
