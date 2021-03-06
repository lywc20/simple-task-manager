package task

import (
	"database/sql"
	"fmt"
	"github.com/hauke96/simple-task-manager/server/util"
	"github.com/lib/pq"
	"github.com/pkg/errors"
	"strconv"
)

type taskRow struct {
	id               int
	processPoints    int
	maxProcessPoints int
	geometry         string
	assignedUser     string
}

type storePg struct {
	*util.Logger
	tx    *sql.Tx
	table string
}

var (
	returnValues = "id, process_points, max_process_points, geometry, assigned_user"
)

func getStore(tx *sql.Tx, logger *util.Logger) *storePg {
	return &storePg{
		Logger: logger,
		tx:     tx,
		table:  "tasks",
	}
}

func (s *storePg) getTasks(projectId string) ([]*Task, error) {
	query := fmt.Sprintf("SELECT id,process_points,max_process_points,geometry,assigned_user FROM %s WHERE project_id = $1;", s.table)
	s.LogQuery(query, projectId)

	rows, err := s.tx.Query(query, projectId)
	if err != nil {
		return nil, errors.Wrapf(err, "error executing query to get tasks for project %s", projectId)
	}
	defer rows.Close()

	// Read all tasks from the returned rows of the query
	tasks := make([]*Task, 0)
	for rows.Next() {
		task, err := rowToTask(rows)
		if err != nil {
			return nil, errors.Wrap(err, "error converting row to task")
		}

		tasks = append(tasks, task)
	}

	if len(tasks) == 0 {
		return nil, errors.New("Tasks do not exist")
	}

	return tasks, nil
}

func (s *storePg) getTask(taskId string) (*Task, error) {
	query := fmt.Sprintf("SELECT id,process_points,max_process_points,geometry,assigned_user FROM %s WHERE id = $1;", s.table)
	s.LogQuery(query, taskId)

	rows, err := s.tx.Query(query, taskId)
	if err != nil {
		return nil, errors.Wrapf(err, "error executing query to get task %s", taskId)
	}
	defer rows.Close()

	if !rows.Next() {
		return nil, errors.New("there is no next row or an error happened")
	}

	task, err := rowToTask(rows)
	if err != nil {
		return nil, errors.Wrap(err, "error converting row to task")
	}

	return task, nil
}

func (s *storePg) addTasks(newTasks []*Task, projectId string) ([]*Task, error) {
	taskIds := make([]string, 0)

	// TODO Do not add one by one but instead build one large query (otherwise it's really slow)
	for _, t := range newTasks {
		id, err := s.addTask(t, projectId)
		if err != nil {
			s.Err("error adding task '%s'", t.Id)
			return nil, err
		}

		taskIds = append(taskIds, id)
	}

	return s.getTasks(projectId)
}

func (s *storePg) addTask(task *Task, projectId string) (string, error) {
	query := fmt.Sprintf("INSERT INTO %s(process_points, max_process_points, geometry, assigned_user, project_id) VALUES($1, $2, $3, $4, $5) RETURNING %s;", s.table, returnValues)
	t, err := s.execQuery(query, task.ProcessPoints, task.MaxProcessPoints, task.Geometry, task.AssignedUser, projectId)

	if err != nil {
		return "", err
	}

	return t.Id, nil
}

func (s *storePg) assignUser(taskId, userId string) (*Task, error) {
	query := fmt.Sprintf("UPDATE %s SET assigned_user=$1 WHERE id=$2 RETURNING %s;", s.table, returnValues)
	return s.execQuery(query, userId, taskId)
}

func (s *storePg) unassignUser(taskId string) (*Task, error) {
	query := fmt.Sprintf("UPDATE %s SET assigned_user='' WHERE id=$1 RETURNING %s;", s.table, returnValues)
	return s.execQuery(query, taskId)
}

func (s *storePg) setProcessPoints(taskId string, newPoints int) (*Task, error) {
	query := fmt.Sprintf("UPDATE %s SET process_points=$1 WHERE id=$2 RETURNING %s;", s.table, returnValues)
	return s.execQuery(query, newPoints, taskId)
}

func (s *storePg) delete(taskIds []string) error {
	query := fmt.Sprintf("DELETE FROM %s WHERE id=ANY($1)", s.table)

	s.LogQuery(query, taskIds)
	_, err := s.tx.Exec(query, pq.Array(taskIds))
	if err != nil {
		return err
	}

	return nil
}

// execQuery executed the given query, turns the result into a Task object and closes the query.
func (s *storePg) execQuery(query string, params ...interface{}) (*Task, error) {
	s.LogQuery(query, params...)
	rows, err := s.tx.Query(query, params...)
	if err != nil {
		return nil, errors.Wrap(err, "could not run query")
	}
	defer rows.Close()

	rows.Next()
	t, err := rowToTask(rows)

	if t == nil && err == nil {
		return nil, errors.New(fmt.Sprintf("Task does not exist"))
	}

	return t, err
}

// rowToTask turns the current row into a Task object. This does not close the row.
func rowToTask(rows *sql.Rows) (*Task, error) {
	var task taskRow
	err := rows.Scan(&task.id, &task.processPoints, &task.maxProcessPoints, &task.geometry, &task.assignedUser)
	if err != nil {
		return nil, errors.Wrap(err, "could not scan rows")
	}

	result := Task{}

	result.Id = strconv.Itoa(task.id)
	result.ProcessPoints = task.processPoints
	result.MaxProcessPoints = task.maxProcessPoints
	result.AssignedUser = task.assignedUser
	result.Geometry = task.geometry

	return &result, err
}
