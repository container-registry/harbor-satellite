package handlers

import (
	"database/sql"
	"encoding/json"
	"time"

	"github.com/container-registry/harbor-satellite/internal/groundcontrol/database"
	swaggermodels "github.com/container-registry/harbor-satellite/internal/groundcontrol/swagger/models"
	"github.com/go-openapi/strfmt"
)

func dateTime(t time.Time) strfmt.DateTime {
	return strfmt.DateTime(t)
}

func nullString(v sql.NullString) *swaggermodels.NullString {
	return &swaggermodels.NullString{String: v.String, Valid: v.Valid}
}

func nullTime(v sql.NullTime) *swaggermodels.NullTime {
	return &swaggermodels.NullTime{Time: dateTime(v.Time), Valid: v.Valid}
}

func nullInt64(v sql.NullInt64) *swaggermodels.NullInt64 {
	return &swaggermodels.NullInt64{Int64: v.Int64, Valid: v.Valid}
}

func nullInt32(v sql.NullInt32) *swaggermodels.NullInt32 {
	return &swaggermodels.NullInt32{Int32: v.Int32, Valid: v.Valid}
}

func databaseConfig(row database.Config) *swaggermodels.APIDatabaseConfig {
	var decoded any
	if len(row.Config) > 0 {
		if err := json.Unmarshal(row.Config, &decoded); err != nil {
			decoded = nil
		}
	}
	return &swaggermodels.APIDatabaseConfig{
		ID:          row.ID,
		ConfigName:  row.ConfigName,
		RegistryURL: row.RegistryUrl,
		Config:      decoded,
		CreatedAt:   dateTime(row.CreatedAt),
		UpdatedAt:   dateTime(row.UpdatedAt),
	}
}

func apiGroup(row database.Group) *swaggermodels.APIGroup {
	return &swaggermodels.APIGroup{
		ID:          row.ID,
		GroupName:   row.GroupName,
		RegistryURL: row.RegistryUrl,
		Projects:    row.Projects,
		CreatedAt:   dateTime(row.CreatedAt),
		UpdatedAt:   dateTime(row.UpdatedAt),
	}
}

func apiGroupSatellite(row database.GetSatellitesByGroupNameRow) *swaggermodels.APIGroupSatellite {
	return &swaggermodels.APIGroupSatellite{
		ID:        row.ID,
		Name:      row.Name,
		CreatedAt: dateTime(row.CreatedAt),
		UpdatedAt: dateTime(row.UpdatedAt),
	}
}

func apiSatellite(row database.Satellite) *swaggermodels.APISatellite {
	return &swaggermodels.APISatellite{
		ID:                row.ID,
		Name:              row.Name,
		CreatedAt:         dateTime(row.CreatedAt),
		UpdatedAt:         dateTime(row.UpdatedAt),
		LastSeen:          nullTime(row.LastSeen),
		HeartbeatInterval: nullString(row.HeartbeatInterval),
	}
}

func apiActiveSatellite(row database.GetActiveSatellitesRow) *swaggermodels.APIActiveSatellite {
	return &swaggermodels.APIActiveSatellite{
		ID:                row.ID,
		Name:              row.Name,
		CreatedAt:         dateTime(row.CreatedAt),
		UpdatedAt:         dateTime(row.UpdatedAt),
		LastSeen:          nullTime(row.LastSeen),
		HeartbeatInterval: nullString(row.HeartbeatInterval),
		LastActivity:      row.LastActivity,
		LastStatusTime:    dateTime(row.LastStatusTime),
	}
}

func apiStaleSatellite(row database.GetStaleSatellitesRow) *swaggermodels.APIStaleSatellite {
	return &swaggermodels.APIStaleSatellite{
		ID:                row.ID,
		Name:              row.Name,
		CreatedAt:         dateTime(row.CreatedAt),
		UpdatedAt:         dateTime(row.UpdatedAt),
		LastSeen:          nullTime(row.LastSeen),
		HeartbeatInterval: nullString(row.HeartbeatInterval),
		SecondsSinceSeen:  row.SecondsSinceSeen,
	}
}

func apiSatelliteStatus(row database.SatelliteStatus) *swaggermodels.APISatelliteStatus {
	return &swaggermodels.APISatelliteStatus{
		ID:                 row.ID,
		SatelliteID:        row.SatelliteID,
		Activity:           row.Activity,
		LatestStateDigest:  nullString(row.LatestStateDigest),
		LatestConfigDigest: nullString(row.LatestConfigDigest),
		CPUPercent:         nullString(row.CpuPercent),
		MemoryUsedBytes:    nullInt64(row.MemoryUsedBytes),
		StorageUsedBytes:   nullInt64(row.StorageUsedBytes),
		LastSyncDurationMs: nullInt64(row.LastSyncDurationMs),
		ImageCount:         nullInt32(row.ImageCount),
		ReportedAt:         dateTime(row.ReportedAt),
		CreatedAt:          dateTime(row.CreatedAt),
		ArtifactIds:        row.ArtifactIds,
	}
}

func apiArtifact(row database.Artifact) *swaggermodels.APIDatabaseArtifact {
	return &swaggermodels.APIDatabaseArtifact{
		ID:        row.ID,
		Reference: row.Reference,
		SizeBytes: row.SizeBytes,
		CreatedAt: dateTime(row.CreatedAt),
	}
}
