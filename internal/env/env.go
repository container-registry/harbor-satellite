package env

import (
	envparser "github.com/caarlos0/env/v11"
)

var (
	GC        GroundControl
	Satellite HarborSatellite
)

func LoadGC() error {
	var cfg GroundControl
	if err := envparser.Parse(&cfg); err != nil {
		return err
	}
	GC = cfg
	return nil
}

func LoadSatellite() error {
	var cfg HarborSatellite
	if err := envparser.Parse(&cfg); err != nil {
		return err
	}
	Satellite = cfg
	return nil
}
