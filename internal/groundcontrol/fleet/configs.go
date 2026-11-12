package fleet

import (
	"context"
	"encoding/json"
	"fmt"

	jsonpatch "github.com/evanphx/json-patch"
)

type ConfigService struct{ c *core }

type CreateConfigCmd struct {
	Name     string
	Document json.RawMessage
}

// UpdateConfigCmd applies Patch as a JSON Merge Patch (RFC 7386).
type UpdateConfigCmd struct {
	Name  string
	Patch json.RawMessage
}

type AssignConfigCmd struct {
	Satellite, Config string
}

// Create and Update run Deps.ConfigValidator on the document they store,
// the merged one for Update. A validation error is a
// *ConfigValidationError and nothing is stored; warnings are stored with
// the document and returned (plan item 7.4).
func (s *ConfigService) Create(ctx context.Context, cmd CreateConfigCmd) (ConfigWriteResult, error) {
	if !ValidName(cmd.Name) {
		return ConfigWriteResult{}, fmt.Errorf("%w: config name %q", ErrInvalid, cmd.Name)
	}
	if err := s.c.authorize(ctx, ActionCreate, configResource(cmd.Name)); err != nil {
		return ConfigWriteResult{}, err
	}
	warnings, err := s.validate(ctx, cmd.Document)
	if err != nil {
		return ConfigWriteResult{}, err
	}
	var stored Config
	err = s.c.inTx(ctx, func(tx Tx) error {
		var err error
		stored, err = tx.Configs().Create(ctx, Config{Name: cmd.Name, RegistryURL: s.c.settings.RegistryURL, Document: cmd.Document})
		if err != nil {
			return fmt.Errorf("create config %s: %w", cmd.Name, err)
		}
		return tx.Events().Append(ctx, configEvent(EventConfigChanged, stored))
	})
	if err != nil {
		return ConfigWriteResult{}, err
	}
	return ConfigWriteResult{Config: stored, Warnings: warnings}, nil
}

// Update merges the patch into the stored document. The update is guarded
// by the version it read, so a concurrent update is ErrConflict instead of
// a lost update (plan item 3.4).
func (s *ConfigService) Update(ctx context.Context, cmd UpdateConfigCmd) (ConfigWriteResult, error) {
	if !ValidName(cmd.Name) {
		return ConfigWriteResult{}, fmt.Errorf("%w: config name %q", ErrInvalid, cmd.Name)
	}
	if err := s.c.authorize(ctx, ActionUpdate, configResource(cmd.Name)); err != nil {
		return ConfigWriteResult{}, err
	}
	var (
		stored, previous Config
		warnings         []string
	)
	err := s.c.inTx(ctx, func(tx Tx) error {
		existing, err := tx.Configs().GetByName(ctx, cmd.Name)
		if err != nil {
			return fmt.Errorf("config %s: %w", cmd.Name, err)
		}
		previous = existing
		merged, err := jsonpatch.MergePatch(existing.Document, cmd.Patch)
		if err != nil {
			return fmt.Errorf("%w: apply merge patch: %w", ErrInvalid, err)
		}
		if warnings, err = s.validate(ctx, merged); err != nil {
			return err
		}
		existing.Document = merged
		existing.RegistryURL = s.c.settings.RegistryURL
		if stored, err = tx.Configs().Update(ctx, existing); err != nil {
			return fmt.Errorf("update config %s: %w", cmd.Name, err)
		}
		return tx.Events().Append(ctx, configEvent(EventConfigChanged, stored))
	})
	if err != nil {
		return ConfigWriteResult{}, err
	}
	return ConfigWriteResult{Config: stored, Warnings: warnings, Previous: previous}, nil
}

func (s *ConfigService) validate(ctx context.Context, doc json.RawMessage) ([]string, error) {
	if s.c.validator == nil {
		return nil, nil
	}
	warnings, err := s.c.validator.ValidateConfig(ctx, doc)
	if err != nil {
		return nil, &ConfigValidationError{Warnings: warnings, Err: fmt.Errorf("%w: %w", ErrInvalid, err)}
	}
	return warnings, nil
}

func configEvent(kind EventKind, c Config) Event {
	return Event{Kind: kind, Aggregate: Aggregate{Type: AggregateConfig, ID: int64(c.ID)}, Payload: ChangedPayload{Name: c.Name}}
}

// Delete fails with ErrInUse while a satellite uses the config.
func (s *ConfigService) Delete(ctx context.Context, name string) error {
	stateRef, err := s.c.settings.ConfigStateRef(name)
	if err != nil {
		return err
	}
	if err := s.c.authorize(ctx, ActionDelete, configResource(name)); err != nil {
		return err
	}
	return s.c.inTx(ctx, func(tx Tx) error {
		cfg, err := tx.Configs().GetByName(ctx, name)
		if err != nil {
			return fmt.Errorf("config %s: %w", name, err)
		}
		inUse, err := tx.Configs().InUse(ctx, cfg.ID)
		if err != nil {
			return fmt.Errorf("config %s in use: %w", name, err)
		}
		if inUse {
			return fmt.Errorf("config %s: %w", name, ErrInUse)
		}
		if err := tx.Configs().Delete(ctx, cfg.ID); err != nil {
			return fmt.Errorf("delete config %s: %w", name, err)
		}
		return tx.Events().Append(ctx, Event{
			Kind:      EventConfigDeleted,
			Aggregate: Aggregate{Type: AggregateConfig, ID: int64(cfg.ID)},
			Payload:   ConfigDeletedPayload{Name: cfg.Name, StateRef: stateRef},
		})
	})
}

func (s *ConfigService) Get(ctx context.Context, name string) (Config, error) {
	if err := s.c.authorize(ctx, ActionRead, configResource(name)); err != nil {
		return Config{}, err
	}
	cfg, err := s.c.read().Configs().GetByName(ctx, name)
	if err != nil {
		return Config{}, fmt.Errorf("config %s: %w", name, err)
	}
	return cfg, nil
}

func (s *ConfigService) List(ctx context.Context) ([]Config, error) {
	configs, err := s.c.read().Configs().List(ctx)
	if err != nil {
		return nil, err
	}
	return filterAllowed(ctx, s.c, ResourceConfig, configs, func(c Config) Resource { return configResource(c.Name) })
}

func configResource(name string) Resource {
	return Resource{Type: ResourceConfig, ID: name}
}

// Assign needs assign_config on the satellite and add_member on each of
// its groups, so a scoped admin cannot move a satellite it shares with
// another group's admin onto a config of its choice.
func (s *ConfigService) Assign(ctx context.Context, cmd AssignConfigCmd) error {
	if !s.c.allowsAll() {
		_, groups, err := s.c.satelliteScope(ctx, s.c.read(), cmd.Satellite)
		if err != nil {
			return err
		}
		checks := []check{{ActionAssignConfig, satelliteResource(cmd.Satellite, groups)}, {ActionRead, configResource(cmd.Config)}}
		for _, g := range groups {
			checks = append(checks, check{ActionAddMember, Resource{Type: ResourceGroup, ID: g}})
		}
		if err := s.c.authorizeAll(ctx, checks...); err != nil {
			return err
		}
	}
	return s.c.inTx(ctx, func(tx Tx) error {
		sat, err := tx.Satellites().GetByName(ctx, cmd.Satellite)
		if err != nil {
			return fmt.Errorf("satellite %s: %w", cmd.Satellite, err)
		}
		cfg, err := tx.Configs().GetByName(ctx, cmd.Config)
		if err != nil {
			return fmt.Errorf("config %s: %w", cmd.Config, err)
		}
		if err := tx.Satellites().SetConfig(ctx, sat.ID, cfg.ID); err != nil {
			return fmt.Errorf("assign config %s to %s: %w", cmd.Config, cmd.Satellite, err)
		}
		return tx.Events().Append(ctx, satelliteEvent(EventAssignmentChanged, sat))
	})
}
