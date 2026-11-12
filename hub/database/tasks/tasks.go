package tasks

import (
	"time"

	"github.com/dengyie/mangohub/hub/database/dbcore"
	"github.com/dengyie/mangohub/hub/database/models"
)

func CreateTask(taskId string, clients []string, command string) error {
	db := dbcore.GetDBInstance()
	// Create a new task in the database with clients as JSON array
	task := models.Task{
		TaskId:  taskId,
		Clients: models.StringArray(clients),
		Command: command,
	}
	if err := db.Create(&task).Error; err != nil {
		return err
	}
	var taskResults []models.TaskResult
	for _, client := range clients {
		taskResults = append(taskResults, models.TaskResult{
			TaskId:     taskId,
			Client:     client,
			Result:     "",
			ExitCode:   nil,
			FinishedAt: nil,
			CreatedAt:  time.Now().UTC(),
		})
	}
	if len(taskResults) > 0 {
		return db.Create(&taskResults).Error
	}
	return nil
}
func GetTaskByTaskId(taskId string) (*models.Task, error) {
	var task models.Task
	if err := dbcore.GetDBInstance().Where("task_id = ?", taskId).First(&task).Error; err != nil {
		return nil, err
	}
	return &task, nil
}
func GetTasksByClientId(clientId string) ([]models.Task, error) {
	var tasks []models.Task
	if err := dbcore.GetDBInstance().Where("clients LIKE ?", "%"+clientId+"%").Find(&tasks).Error; err != nil {
		return nil, err
	}
	return tasks, nil
}

func GetSpecificTaskResult(taskId, clientId string) (*models.TaskResult, error) {
	var result models.TaskResult
	if err := dbcore.GetDBInstance().Where("task_id = ? AND client = ?", taskId, clientId).First(&result).Error; err != nil {
		return nil, err
	}
	return &result, nil
}

func GetAllTasks() ([]models.Task, error) {
	var tasks []models.Task
	if err := dbcore.GetDBInstance().Find(&tasks).Error; err != nil {
		return nil, err
	}
	return tasks, nil
}

func GetTaskResultsByTaskId(taskId string) ([]models.TaskResult, error) {
	var results []models.TaskResult
	if err := dbcore.GetDBInstance().Where("task_id = ?", taskId).Find(&results).Error; err != nil {
		return nil, err
	}
	return results, nil
}
func SaveTaskResult(taskId, clientId, result string, exitCode int, timestamp time.Time) error {
	return dbcore.GetDBInstance().
		Model(&models.TaskResult{}).
		Where("task_id = ? AND client = ?", taskId, clientId).
		Updates(map[string]interface{}{
			"result":      result,
			"exit_code":   exitCode,
			"finished_at": timestamp.UTC(),
		}).Error
}

func ClearTaskResultsByTimeBefore(before time.Time) error {
	return dbcore.GetDBInstance().Where("created_at < ?", before.UTC()).Delete(&models.TaskResult{}).Error
}

// FinalizeStaleNullResults 把超过 maxAge 仍未回传的任务行收敛为 -1 终态。
// CreateTask 预建 NULL exit_code 占位行，正常由 agent taskResult 或分发失败
// 分支回写；但排队事件（TTL 5min）过期被静默丢弃、agent 半路掉线等场景会让
// 占位行永久悬空，前端只能看到永远「运行中」。本函数是对账兜底，不碰已回传行。
func FinalizeStaleNullResults(maxAge time.Duration, before time.Time) (int64, error) {
	result := dbcore.GetDBInstance().
		Model(&models.TaskResult{}).
		Where("exit_code IS NULL AND created_at < ?", before.UTC().Add(-maxAge)).
		Updates(map[string]interface{}{
			"result":      "Dispatch failed: task expired without agent response",
			"exit_code":   -1,
			"finished_at": time.Now().UTC(),
		})
	return result.RowsAffected, result.Error
}
