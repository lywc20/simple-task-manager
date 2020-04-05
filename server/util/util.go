package util

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
)

var (
	nextId = 0
)

func GetId() string {
	id := nextId
	nextId += 1
	return strconv.Itoa(id)
}

func GetParam(param string, r *http.Request) (string, error) {
	value := r.FormValue(param)
	if strings.TrimSpace(value) == "" {
		errMsg := fmt.Sprintf("Parameter '%s' not specified", param)
		return "", errors.New(errMsg)
	}

	return value, nil
}

func GetIntParam(param string, w http.ResponseWriter, r *http.Request) (int, error) {
	valueString, err := GetParam(param, r)
	if err != nil {
		return 0, err
	}

	return strconv.Atoi(valueString)
}

func ResponseBadRequest(w http.ResponseWriter, err string) {
	w.WriteHeader(http.StatusBadRequest)
	w.Write([]byte(err))
}

func ResponseInternalError(w http.ResponseWriter, err string) {
	w.WriteHeader(http.StatusInternalServerError)
	w.Write([]byte(err))
}

func Response(w http.ResponseWriter, data string, status int) {
	w.WriteHeader(status)
	w.Write([]byte(data))
}