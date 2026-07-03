package harborhealth

type Component struct {
	Name   string `json:"name"`
	Status string `json:"status"`
	Error  string `json:"error,omitempty"`
}

func (c *Component) IsHealthy() bool {
	return c.Status == "healthy"
}

type HealthResponse struct {
	Components []Component `json:"components"`
	Status     string      `json:"status"`
}

func (h *HealthResponse) GetUnhealthyComponents(skip map[string]struct{}) []string {
	var unhealthy []string
	for _, c := range h.Components {
		if _, ignore := skip[c.Name]; ignore {
			continue
		}
		if !c.IsHealthy() {
			unhealthy = append(unhealthy, c.Name, c.Error)
		}
	}
	return unhealthy
}
