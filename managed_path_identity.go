package main

import (
	"strconv"
	"github.com/ayanmw/CloudHarbor/internal/downloader"
)

type managedPathImpactStringTask struct {
	downloader.ManagedPathImpactTask
	FsID string `json:"fs_id"`
}

func managedPathImpactResponse(impact downloader.ManagedPathImpact, stringIDs bool) any {
	if !stringIDs {
		return impact
	}
	tasks := make([]managedPathImpactStringTask, len(impact.Tasks))
	for index, task := range impact.Tasks {
		tasks[index] = managedPathImpactStringTask{ManagedPathImpactTask: task, FsID: strconv.FormatInt(task.FsID, 10)}
	}
	return struct {
		downloader.ManagedPathImpact
		Tasks []managedPathImpactStringTask `json:"tasks"`
	}{ManagedPathImpact: impact, Tasks: tasks}
}
