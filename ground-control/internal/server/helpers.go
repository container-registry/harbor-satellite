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

func validateRequestBody(w http.ResponseWriter, req RegisterSatelliteParams) error {
	if len(req.Name) < 1 {
		log.Println("name should be at least one character long.")
		return &AppError{
			Message: "Error: name should be at least one character long.",
			Code:    http.StatusBadRequest,
		}
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
		return &AppError{
			Message: fmt.Sprintf("Error querying for robot account: %v", err.Error()),
			Code:    http.StatusBadRequest,
		}
	}
	if roboPresent {
		return &AppError{
			Message: "Error: Robot Account name already present. Try with a different name.",
			Code:    http.StatusBadRequest,
		}
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
					return nil, &AppError{
						Message: fmt.Sprintf("Error: Group Name: %s, does not exist in replication, Please give a Valid Group Name", groupName),
						Code:    http.StatusBadRequest,
					}
				}
			}
			group, err := q.GetGroupByName(ctx, groupName)
			if err != nil {
				return nil, &AppError{
					Message: fmt.Sprintf("Error: Invalid Group Name: %v", groupName),
					Code:    http.StatusBadRequest,
				}
			}
			// TODO: we just need the group id here.
			if err := q.AddSatelliteToGroup(ctx, database.AddSatelliteToGroupParams{
				SatelliteID: satelliteID,
				GroupID:     group.ID,
			}); err != nil {
				return nil, err
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
		return &AppError{
			Message: fmt.Sprintf("Error: Checking satellite project: %v", err),
			Code:    http.StatusBadGateway,
		}
	}
	if !satExist {
		_, err := harbor.CreateSatelliteProject(ctx)
		if err != nil {
			log.Println(err)
			return &AppError{
				Message: fmt.Sprintf("Error: creating satellite project: %v", err),
				Code:    http.StatusBadGateway,
			}
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
		return &AppError{
			Message: fmt.Sprintf("Error: adding robot account to DB %v", err.Error()),
			Code:    http.StatusInternalServerError,
		}
	}
	return nil
}

func assignPermissionsToRobot(ctx context.Context, q *database.Queries, groups *[]string, robotID int64) error {
	if groups != nil {
		for _, groupName := range *groups {
			projects, err := q.GetProjectsOfGroup(ctx, groupName)
			if err != nil {
				log.Println(err)
				return &AppError{
					Message: fmt.Sprintf("Error: fetching projects of group %v", err.Error()),
					Code:    http.StatusInternalServerError,
				}
			}

			project := projects[0]
			// give permission to the robot account for all the projects present in this group
			_, err = utils.UpdateRobotProjects(ctx, project, strconv.FormatInt(robotID, 10))
			if err != nil {
				log.Println(err)
				return &AppError{
					Message: fmt.Sprintf("Error: updating robot account %v", err.Error()),
					Code:    http.StatusInternalServerError,
				}
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
			return nil, &AppError{
				Message: "Error: Get Group By ID Failed",
				Code:    http.StatusInternalServerError,
			}
		}
		state := utils.AssembleGroupState(grp.GroupName)
		states = append(states, state)
	}
	return states, nil
}

func DecodeRequestBody(r *http.Request, v interface{}) error {
	if err := json.NewDecoder(r.Body).Decode(v); err != nil {
		return &AppError{
			Message: "Invalid request body",
			Code:    http.StatusBadRequest,
		}
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
		err := &AppError{
			Message: "Authorization header missing",
			Code:    http.StatusUnauthorized,
		}
		return "", err
	}

	parts := strings.Split(authHeader, " ")
	if len(parts) != 2 || strings.ToLower(parts[0]) != "bearer" {
		err := &AppError{
			Message: "Invalid Authorization header format",
			Code:    http.StatusUnauthorized,
		}
		return "", err
	}
	token := parts[1]

	return token, nil
}
