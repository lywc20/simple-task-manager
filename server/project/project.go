package project

import (
	"database/sql"
	"fmt"
	"github.com/hauke96/sigolo"
	"github.com/pkg/errors"

	"github.com/hauke96/simple-task-manager/server/config"
	"github.com/hauke96/simple-task-manager/server/task"
)

type Project struct {
	Id              string   `json:"id"`
	Name            string   `json:"name"`
	TaskIDs         []string `json:"taskIds"`
	Users           []string `json:"users"`
	Owner           string   `json:"owner"`
	Description     string   `json:"description"`
	NeedsAssignment bool     `json:"needsAssignment"` // When "true", the tasks of this project need to have an assigned user
}

type store interface {
	init(db *sql.DB)
	getProjects(user string) ([]*Project, error)
	getProject(id string) (*Project, error)
	getProjectByTask(taskId string) (*Project, error)
	addProject(draft *Project, user string) (*Project, error)
	addUser(userToAdd string, id string, owner string) (*Project, error)
	removeUser(id string, userToRemove string) (*Project, error)
	delete(id string) error
	getTasks(id string) ([]*task.Task, error)
}

var (
	projectStore         store
	maxDescriptionLength = 10000
)

func Init() {
	if config.Conf.Store == "postgres" {
		db, err := sql.Open("postgres", "user=postgres password=geheim dbname=stm sslmode=disable")
		sigolo.FatalCheck(err)

		projectStore = &storePg{}
		projectStore.init(db)
	} else if config.Conf.Store == "cache" {
		projectStore = &storeLocal{}
		projectStore.init(nil)
	}
}

func GetProjects(user string) ([]*Project, error) {
	return projectStore.getProjects(user)
}

// AddProject adds the project, as requested by user "user".
func AddProject(project *Project, user string) (*Project, error) {
	if project.Id != "" {
		return nil, errors.New("Id not empty")
	}

	if project.Owner == "" {
		return nil, errors.New("Owner must be set")
	}

	usersContainOwner := false
	for _, u := range project.Users {
		usersContainOwner = usersContainOwner || (u == project.Owner)
	}

	if !usersContainOwner {
		return nil, errors.New("Owner must be within users list")
	}

	if project.Name == "" {
		return nil, errors.New("Project must have a title")
	}

	if len(project.TaskIDs) == 0 {
		return nil, errors.New("No tasks have been specified")
	}

	if len(project.Description) > maxDescriptionLength {
		return nil, errors.New(fmt.Sprintf("Description too long. Maximum allowed are %d characters.", maxDescriptionLength))
	}

	return projectStore.addProject(project, user)
}

func GetProject(id string, potentialMember string) (*Project, error) {
	// TODO remove use permission service
	// Check if user is a member of the project. If not, throw error
	//userIsMember, err := IsUserInProject(id, potentialMember)
	//if err != nil {
	//	return nil, errors.Wrap(err, "could not get project: user membership verification failed")
	//}
	//
	//if !userIsMember {
	//	return nil, fmt.Errorf("the user '%s' is not a member of the project '%s'", potentialMember, id)
	//}

	return projectStore.getProject(id)
}

func GetProjectByTask(taskId string, potentialMember string) (*Project, error) {
	project, err:= projectStore.getProjectByTask(taskId)
	
	if err != nil {
		return nil, errors.Wrap(err, "error getting project")
	}

	// TODO remove and use permission service
	//userIsMember, err := projectStore.verifyMembership(project.Id, potentialMember)
	//
	//if err != nil {
	//	return nil, errors.Wrap(err, "could not get project: user membership verification failed")
	//}
	//
	//if !userIsMember{
	//	return nil, errors.New(fmt.Sprintf("user %s is not a member of project %s, where the task %s belongs to", potentialMember, project.Id, taskId))
	//}
	
	return project, nil
}

func AddUser(user, id, potentialOwner string) (*Project, error) {
	p, err := projectStore.getProject(id)
	if err != nil {
		return nil, errors.Wrap(err, "unable to get project to add user")
	}

	// Only the owner is allowed to invite
	if p.Owner != potentialOwner {
		return nil, fmt.Errorf("user '%s' is not allowed to add another user", potentialOwner)
	}

	// Check if user is already in project. If so, just do nothing and return
	for _, u := range p.Users {
		if u == user {
			return p, errors.New("User already added")
		}
	}

	return projectStore.addUser(user, id, potentialOwner)
}

func LeaveProject(id string, potentialMember string) (*Project, error) {
	p, err := projectStore.getProject(id)
	if err != nil {
		return nil, errors.Wrap(err, "could not get project")
	}

	// The owner can only delete a project but not leave it
	if p.Owner == potentialMember {
		return nil, fmt.Errorf("the owner '%s' is not allowed to leave the project '%s'", potentialMember, p.Id)
	}

	// TODO remove use permission service
	// Check if user is a member of the project. If not, throw error
	//userIsMember, err := IsUserInProject(id, potentialMember)
	//if err != nil {
	//	return nil, errors.Wrap(err, fmt.Sprintf("cannot remove user %s from project: membership verification failed", potentialMember))
	//}
	//
	//if !userIsMember {
	//	return nil, fmt.Errorf("the user '%s' is not a member of the project '%s'", potentialMember, p.Id)
	//}

	return projectStore.removeUser(id, potentialMember)
}

func RemoveUser(id, requestingUser, userToRemove string) (*Project, error) {
	p, err := projectStore.getProject(id)
	if err != nil {
		return nil, errors.Wrap(err, "could not get project")
	}

	// When a user tries to remove a different user, only the owner is allowed to do that
	if requestingUser != userToRemove && p.Owner != requestingUser {
		return nil, fmt.Errorf("user '%s' is not allowed to remove another user", requestingUser)
	}

	// TODO remove and use permission service
	// Check if user is already in project. If so, just do nothing and return
	//projectContainsUser,err := projectStore.verifyMembership(id, userToRemove)
	//
	//if err != nil {
	//	return nil, errors.Wrap(err, "cannot remove user: verification of membership failed")
	//}
	//
	//if !projectContainsUser {
	//	return nil, errors.New("cannot remove user: the user is not a member of the project")
	//}

	return projectStore.removeUser(id, userToRemove)
}

// VerifyOwnership checks whether all given tasks are part of projects where the
// given user is a member of. In other words: This function checks if the user
// has the permission to change each task.
func VerifyOwnership(user string, taskIds []string) (bool, error) {
	// Only look at projects the user is part of. We then need less checks below
	userProjects, err := GetProjects(user)
	if err != nil {
		return false, errors.Wrap(err, "could not get projects to verify ownership")
	}

	for _, taskId := range taskIds {
		found := false

		for _, project := range userProjects {
			for _, t := range project.TaskIDs {
				found = t == taskId

				if found {
					break
				}
			}

			if found {
				break
			}
		}

		// We went through all projects the given user is member of and we didn't
		// find a match. The user is therefore not allowed to view the current
		// task and we can abort here.
		if !found {
			return false, nil
		}
	}

	return true, nil
}

func DeleteProject(id, potentialOwner string) error {
	p, err := projectStore.getProject(id)
	if err != nil {
		return errors.Wrap(err, "could not get project")
	}

	// Only the owner can delete a project
	if p.Owner != potentialOwner {
		return fmt.Errorf("the user '%s' is not the owner of project '%s'", potentialOwner, p.Id)
	}

	err = projectStore.delete(id)
	if err != nil {
		return errors.Wrap(err, "could not remove project")
	}

	return nil
}

func GetTasks(id string, user string) ([]*task.Task, error) {
	_, err := projectStore.getProject(id)
	if err != nil {
		return nil, errors.Wrap(err, "could not get project")
	}

	// TODO remove use permission service
	// Only members of the project can get tasks
	//userIsMember, err := IsUserInProject(id, user)
	//if err != nil {
	//	return nil, errors.Wrap(err, "could not get tasks: user membership verification failed")
	//}
	//
	//if !userIsMember {
	//	return nil, fmt.Errorf("the user '%s' is not a member of the project '%s'", user, p.Id)
	//}

	return projectStore.getTasks(id)
}