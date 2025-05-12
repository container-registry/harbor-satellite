package server

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"

	"github.com/container-registry/harbor-satellite/ground-control/internal/database"
	"github.com/container-registry/harbor-satellite/ground-control/internal/utils"
	"github.com/container-registry/harbor-satellite/ground-control/reg/harbor"
	goharbormodels "github.com/goharbor/go-client/pkg/sdk/v2.0/models"
)

func isConfigInUse(ctx context.Context, q *database.Queries, config database.Config) (bool, error) {
	// Check if any satellite is using this config
	satellites, err := q.ConfigSatelliteList(ctx, config.ID)
	if err != nil {
		return false, err
	}

	// If any entries exist, config is in use
	return len(satellites) > 0, nil
}

func setSatelliteConfig(ctx context.Context, q *database.Queries, satelliteName string, configName string) (*database.Satellite, error) {
	sat, err := q.GetSatelliteByName(ctx, satelliteName)
	if err != nil {
		log.Printf("Error: Satellite Not Found: %v", err)
		return nil, NotFoundError("Satellite", satelliteName, err)
	}

	configObject, err := q.GetConfigByName(ctx, configName)
	if err != nil {
		log.Printf("Error: Config Not Found: %v", err)
		return nil, NotFoundError("Config", configName, err)
	}

	params := database.SetSatelliteConfigParams{
		SatelliteID: int32(sat.ID),
		ConfigID:    int32(configObject.ID),
	}

	err = q.SetSatelliteConfig(ctx, params)
	if err != nil {
		log.Printf("Error: Failed to Set Satellite Config: %v", err)
		return nil, DatabaseError("set satellite config", err)
	}
	return &sat, nil
}

func validateRequestBody(w http.ResponseWriter, req RegisterSatelliteParams) error {
	if len(req.Name) < 1 {
		log.Println("name should be at least one character long.")
		return ValidationError("name should be at least one character long", nil)
	}
	return nil
}

// If the robot account is already present, we need to check if the robot account
// permissions need to be updated.
// i.e, check if the satellite is already connected to the groups in the request body.
// if not, then update the robot account.
func checkRobotAccountExistence(ctx context.Context, name string) error {
	roboPresent, err := harbor.IsRobotPresent(ctx, name)
	if err != nil {
		log.Println(err)
		return ExternalAPIError("Harbor", "querying for robot account", err)
	}
	if roboPresent {
		return ValidationError("Robot Account name already present", nil).
			WithSuggestion("Try with a different name")
	}
	return nil
}

func addSatelliteToGroups(ctx context.Context, q *database.Queries, groups *[]string, satelliteID int32) ([]string, error) {
	var groupStates []string
	if groups != nil {
		for _, groupName := range *groups {
			// check if groups are declared in replication
			replications, err := harbor.ListReplication(ctx, harbor.ListParams{
				Q: fmt.Sprintf("name=%s", groupName),
			})
			if len(replications) < 1 {
				if err != nil {
					log.Println(err)
					return nil, ValidationError(
						fmt.Sprintf("Group Name '%s' does not exist in replication", groupName),
						err,
					).WithSuggestion("Please provide a valid group name")
				}
			}
			group, err := q.GetGroupByName(ctx, groupName)
			if err != nil {
				return nil, NotFoundError("Group", groupName, err)
			}
			// TODO: we just need the group id here.
			if err := q.AddSatelliteToGroup(ctx, database.AddSatelliteToGroupParams{
				SatelliteID: satelliteID,
				GroupID:     group.ID,
			}); err != nil {
				return nil, DatabaseError("add satellite to group", err)
			}

			groupStates = append(groupStates, utils.AssembleGroupState(groupName))
		}
	}
	return groupStates, nil
}

// check if project satellite exists and if does not exist create project satellite
func ensureSatelliteProjectExists(ctx context.Context) error {
	satExist, err := harbor.GetProject(ctx, "satellite")
	if err != nil {
		log.Println(err)
		return ExternalAPIError("Harbor", "checking satellite project", err)
	}
	if !satExist {
		_, err := harbor.CreateSatelliteProject(ctx)
		if err != nil {
			log.Println(err)
			return ExternalAPIError("Harbor", "creating satellite project", err)
		}
	}
	return nil
}

// Add Robot Account to database
func storeRobotAccountInDB(ctx context.Context, q *database.Queries, rbt *goharbormodels.RobotCreated, satelliteID int32) error {
	params := database.AddRobotAccountParams{
		RobotName:   rbt.Name,
		RobotSecret: rbt.Secret,
		RobotID:     strconv.Itoa(int(rbt.ID)),
		SatelliteID: satelliteID,
	}
	if _, err := q.AddRobotAccount(ctx, params); err != nil {
		log.Println(err)
		return DatabaseError("adding robot account to database", err)
	}
	return nil
}

func assignPermissionsToRobot(ctx context.Context, q *database.Queries, groups *[]string, robotID int64) error {
	if groups != nil {
		for _, groupName := range *groups {
			projects, err := q.GetProjectsOfGroup(ctx, groupName)
			if err != nil {
				log.Println(err)
				return DatabaseError("fetching projects of group", err)
			}

			project := projects[0]
			// give permission to the robot account for all the projects present in this group
			_, err = utils.UpdateRobotProjects(ctx, project, strconv.FormatInt(robotID, 10))
			if err != nil {
				log.Println(err)
				return ExternalAPIError("Harbor", "updating robot account permissions", err)
			}

		}
	}
	return nil
}

func getGroupStates(ctx context.Context, groups []database.SatelliteGroup, q *database.Queries) ([]string, error) {
	var states []string
	for _, group := range groups {
		grp, err := q.GetGroupByID(ctx, group.GroupID)
		if err != nil {
			log.Printf("failed to get group by ID: %v, %v", group.GroupID, err)
			return nil, DatabaseError("get group by ID", err)
		}
		state := utils.AssembleGroupState(grp.GroupName)
		states = append(states, state)
	}
	return states, nil
}

func DecodeRequestBody(r *http.Request, v interface{}) error {
	if err := json.NewDecoder(r.Body).Decode(v); err != nil {
		return ValidationError("Invalid request body", err)
	}
	return nil
}

// creates a unique random API token of the specified length in bytes.
func GenerateRandomToken(charLength int) (string, error) {
	// The number of bytes needed to generate a token with the required number of hex characters
	byteLength := charLength / 2

	// Create a byte slice of the required length
	token := make([]byte, byteLength)
	_, err := rand.Read(token)
	if err != nil {
		return "", err
	}

	// Return the token as a hex-encoded string
	return hex.EncodeToString(token), nil
}

func GetAuthToken(r *http.Request) (string, error) {
	authHeader := r.Header.Get("Authorization")
	if authHeader == "" {
		return "", NewAppError(
			"Authorization header missing",
			http.StatusUnauthorized,
			CategorySecurity,
			nil,
		)
	}

	parts := strings.Split(authHeader, " ")
	if len(parts) != 2 || strings.ToLower(parts[0]) != "bearer" {
		return "", NewAppError(
			"Invalid Authorization header format",
			http.StatusUnauthorized,
			CategorySecurity,
			nil,
		).WithSuggestion("Use format: 'Bearer TOKEN'")
	}
	token := parts[1]

	return token, nil
}

func fetchSatelliteConfig(ctx context.Context, dbQueries *database.Queries, satelliteID int32) (database.Config, error) {
	satelliteConfig, err := dbQueries.SatelliteConfig(ctx, satelliteID)
	if err != nil {
		log.Printf("Error: Failed to fetch satellite config: %v", err)
		return database.Config{}, DatabaseError("fetch satellite config", err)
	}

	configObject, err := dbQueries.GetConfigByID(ctx, satelliteConfig.ConfigID)
	if err != nil {
		log.Printf("Error: Failed to fetch satellite config: %v", err)
		return database.Config{}, DatabaseError("fetch config by ID", err)
	}
	return configObject, nil
}

// ValidationError creates a new validation error
func ValidationError(message string, err error) *AppError {
	return NewAppError(
		message,
		http.StatusBadRequest,
		CategoryValidation,
		err,
	).WithSuggestion("Please check your input and try again")
}

// NotFoundError creates a new not found error
func NotFoundError(resourceType string, identifier string, err error) *AppError {
	return NewAppError(
		fmt.Sprintf("%s not found: %s", resourceType, identifier),
		http.StatusNotFound,
		CategoryNotFound,
		err,
	)
}

// DatabaseError creates a new database error
func DatabaseError(operation string, err error) *AppError {
	return NewAppError(
		fmt.Sprintf("Database operation failed: %s", operation),
		http.StatusInternalServerError,
		CategoryDatabase,
		err,
	)
}

// ExternalAPIError creates a new external API error
func ExternalAPIError(service string, operation string, err error) *AppError {
	return NewAppError(
		fmt.Sprintf("%s operation failed", operation),
		http.StatusBadGateway,
		CategoryExternalAPI,
		err,
	).WithSuggestion(fmt.Sprintf("Please try again later or check the %s service", service))
}
