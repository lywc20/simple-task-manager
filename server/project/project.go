package project

import (
	"database/sql"
	"fmt"
	"github.com/hauke96/sigolo"
	"github.com/hauke96/simple-task-manager/server/permission"
	"github.com/pkg/errors"

	"github.com/hauke96/simple-task-manager/server/task"
)

type Project struct {
	Id                 string   `json:"id"`
	Name               string   `json:"name"`
	TaskIDs            []string `json:"taskIds"`
	Users              []string `json:"users"`
	Owner              string   `json:"owner"`
	Description        string   `json:"description"`
	NeedsAssignment    bool     `json:"needsAssignment"`    // When "true", the tasks of this project need to have an assigned user
	TotalProcessPoints int      `json:"totalProcessPoints"` // Sum of all maximum process points of all tasks
	DoneProcessPoints  int      `json:"doneProcessPoints"`  // Sum of all process points that have been set
}

type store interface {
	init(db *sql.DB)
	getProjects(user string) ([]*Project, error)
	getProject(id string) (*Project, error)
	getProjectByTask(taskId string) (*Project, error)
	areTasksUsed(taskIds []string) (bool, error)
	addProject(draft *Project, user string) (*Project, error)
	addUser(userToAdd string, id string, owner string) (*Project, error)
	removeUser(id string, userToRemove string) (*Project, error)
	delete(id string) error
	getTasks(id string, user string) ([]*task.Task, error)
}

var (
	projectStore         store
	maxDescriptionLength = 10000
)

func Init() {
	db, err := sql.Open("postgres", "user=postgres password=geheim dbname=stm sslmode=disable")
	sigolo.FatalCheck(err)

	projectStore = &storePg{}
	projectStore.init(db)
}

func GetProjects(user string) ([]*Project, error) {
	projects, err:= projectStore.getProjects(user)
	if err != nil {
		return nil, err
	}

	for _, p:=range projects {
		err = addProcessPointData(p, user)
		if err != nil {
			return nil, err
		}
	}

	return projects, nil
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

	tasksAlreadyUsed, err := projectStore.areTasksUsed(project.TaskIDs)
	if err != nil {
		return nil, errors.Wrap(err, "error checking whether given tasks are already used")
	}
	if tasksAlreadyUsed {
		return nil, errors.New("The given tasks are already used in other Projects")
	}

	if len(project.Description) > maxDescriptionLength {
		return nil, errors.New(fmt.Sprintf("Description too long. Maximum allowed are %d characters.", maxDescriptionLength))
	}

	return projectStore.addProject(project, user)
}

func GetProject(id string, potentialMember string) (*Project, error) {
	err := permission.VerifyMembershipProject(id, potentialMember)
	if err != nil {
		return nil, errors.Wrap(err, "user membership verification failed")
	}

	project, err := projectStore.getProject(id)
	if err != nil {
		return nil, errors.Wrap(err, "getting project failed")
	}

	err = addProcessPointData(project, potentialMember)
	if err != nil {
		return nil, err
	}

	return project, nil
}

func addProcessPointData(project *Project, potentialMember string) error {
	tasks, err := GetTasks(project.Id, potentialMember)
	if err != nil {
		return errors.Wrap(err, "getting tasks of project failed")
	}

	// Collect the overall finish-state of the project
	for _, t := range tasks {
		sigolo.Info("%s: %d / %d", t.Id, t.ProcessPoints, t.MaxProcessPoints)
		project.DoneProcessPoints += t.ProcessPoints
		project.TotalProcessPoints += t.MaxProcessPoints
	}
	return nil
}

func GetProjectByTask(taskId string, potentialMember string) (*Project, error) {
	err := permission.VerifyMembershipTask(taskId, potentialMember)
	if err != nil {
		return nil, errors.Wrap(err, "user membership verification failed")
	}

	project, err := projectStore.getProjectByTask(taskId)

	if err != nil {
		return nil, errors.Wrap(err, "error getting project")
	}

	return project, nil
}

func AddUser(user, id, potentialOwner string) (*Project, error) {
	err := permission.VerifyOwnership(id, potentialOwner)
	if err != nil {
		return nil, errors.Wrap(err, "user ownership verification failed")
	}

	p, err := projectStore.getProject(id)
	if err != nil {
		return nil, errors.Wrap(err, "unable to get project to add user")
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
	// Only the owner can delete a project but cannot not leave it
	err := permission.VerifyOwnership(id, potentialMember)
	if err == nil {
		return nil, errors.New("the given user is the owner and therefore cannot leave the project")
	}

	err = permission.VerifyMembershipProject(id, potentialMember)
	if err != nil {
		return nil, errors.Wrap(err, "user membership verification failed")
	}

	return projectStore.removeUser(id, potentialMember)
}

func RemoveUser(id, requestingUser, userToRemove string) (*Project, error) {
	// Both users have to be member of the project
	err := permission.VerifyMembershipProject(id, requestingUser)
	if err != nil {
		return nil, errors.Wrap(err, "membership verification of requesting user failed")
	}

	err = permission.VerifyMembershipProject(id, userToRemove)
	if err != nil {
		return nil, errors.Wrap(err, "membership verification of user to remove failed")
	}

	// It's not possible to remove the owner
	err = permission.VerifyOwnership(id, userToRemove)
	if err == nil {
		return nil, errors.New("not allowed to remove owner")
	}

	err = permission.VerifyOwnership(id, requestingUser)
	requestingUserIsOwner := err == nil

	// When a user tries to remove a different user, only the owner is allowed to do that
	if requestingUser != userToRemove && !requestingUserIsOwner {
		return nil, fmt.Errorf("non-owner user '%s' is not allowed to remove another user", requestingUser)
	}

	return projectStore.removeUser(id, userToRemove)
}

// VerifyOwnership checks whether all given tasks are part of projects where the
// given user is a member of. In other words: This function checks if the user
// has the permission to change each task.
// TODO remove when API v1 has been removed
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

func DeleteProject(projectId, potentialOwner string) error {
	err := permission.VerifyOwnership(projectId, potentialOwner)
	if err != nil {
		return errors.Wrap(err, "ownership verification failed")
	}

	project, err := projectStore.getProject(projectId)
	if err != nil {
		return errors.Wrap(err, "unable to read project before removal")
	}

	// First delete the tasks, due to ownership check which won't work, when there's no project anymore.
	task.Delete(project.TaskIDs, potentialOwner)

	// Then remove the project
	err = projectStore.delete(projectId)
	if err != nil {
		return errors.Wrap(err, "could not remove project")
	}

	return nil
}

// TODO move into task package, pass task IDs as parameter and use the permission service to check the permissions on those tasks
func GetTasks(projectId string, user string) ([]*task.Task, error) {
	err := permission.VerifyMembershipProject(projectId, user)
	if err != nil {
		return nil, errors.Wrap(err, "membership verification failed")
	}

	return projectStore.getTasks(projectId, user)
}
