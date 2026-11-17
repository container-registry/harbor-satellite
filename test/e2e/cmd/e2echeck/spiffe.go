package main

import (
	"context"
	"flag"
	"fmt"
	"net/http"
)

// spiffeSetupFlags configures a satellite registered through SPIFFE: the
// SPIFFE registration route takes no config or groups, so this creates a
// config that points the satellite at Ground Control's internal HTTPS URL,
// a group with the test image, and attaches both.
func spiffeSetupFlags(fs *flag.FlagSet) func(ctx context.Context, g *gcClient) error {
	satellite := fs.String("satellite", "", "satellite registered with SPIFFE")
	config := fs.String("config", "", "config to create and assign")
	group := fs.String("group", "", "group to create and join")
	registry := fs.String("registry", "", "Harbor URL inside the E2E network")
	repository := fs.String("repository", "", "repository of the group, tag latest")
	gcInternal := fs.String("gc-internal-url", "", "Ground Control URL inside the E2E network")
	heartbeat := fs.String("heartbeat-interval", "@every 00h00m10s", "heartbeat interval of the config")
	return func(ctx context.Context, g *gcClient) error {
		for name, v := range map[string]string{
			"satellite": *satellite, "config": *config, "group": *group,
			"registry": *registry, "repository": *repository, "gc-internal-url": *gcInternal,
		} {
			if v == "" {
				return fmt.Errorf("-%s is required", name)
			}
		}
		steps := []struct {
			path string
			want int
			body any
		}{
			{"/api/configs", http.StatusCreated, map[string]any{
				"config_name": *config, "registry": *registry,
				"config": map[string]any{"app_config": map[string]any{
					"ground_control_url":          *gcInternal,
					"log_level":                   "info",
					"use_unsecure":                true,
					"state_replication_interval":  "@every 00h00m10s",
					"register_satellite_interval": "@every 00h00m10s",
					"heartbeat_interval":          *heartbeat,
					"bring_own_registry":          false,
				}},
			}},
			{"/api/groups/sync", http.StatusOK, map[string]any{
				"group": *group, "registry": *registry,
				"artifacts": []map[string]any{{"repository": *repository, "tag": []string{"latest"}}},
			}},
			{"/api/configs/satellite", http.StatusOK, map[string]string{"satellite": *satellite, "config_name": *config}},
			{"/api/groups/satellite", http.StatusOK, map[string]string{"satellite": *satellite, "group": *group}},
		}
		for _, step := range steps {
			code, body, err := g.do(ctx, http.MethodPost, step.path, step.body)
			if err != nil {
				return fmt.Errorf("POST %s: %w", step.path, err)
			}
			if code != step.want {
				return fmt.Errorf("POST %s answered %d, want %d: %s", step.path, code, step.want, body)
			}
			fmt.Printf("POST %s: %d\n", step.path, code)
		}
		fmt.Printf("PASS: SPIFFE satellite %s has config %s and group %s\n", *satellite, *config, *group)
		return nil
	}
}
