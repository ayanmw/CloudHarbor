package uploads

import "database/sql"

// Recovery evidence and state must commit together: a restart is not proof of
// remote completion or a resumable upload session. Preserve the prior byte count.
func recoverInterruptedUploads(db *sql.DB) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	at := now()
	stateExpression := `CASE WHEN state IN ('submitting','finalizing') THEN 'unknown' WHEN ` + pendingPauseSQL + ` THEN 'paused' ELSE 'queued' END`
	_, err = tx.Exec(`INSERT INTO events(item,state,sent,recorded,error)
 SELECT id,`+stateExpression+`,sent,?,
 'restart_recovery: previous_state='||state||'; previous_updated_at_utc='||updated||'; action='||
 CASE WHEN state IN ('submitting','finalizing') THEN 'isolated_unconfirmed_remote_submit' WHEN `+pendingPauseSQL+` THEN 'paused_after_restart_request' ELSE 'requeued_without_confirmed_part_resume' END
 FROM items WHERE state IN ('hashing','uploading','submitting','finalizing')`, at)
	if err != nil {
		return err
	}
	_, err = tx.Exec(`UPDATE items SET state=`+stateExpression+`,
 error=CASE WHEN state IN ('submitting','finalizing') THEN '服务重启时远端提交尚未确认，请核对远端；未自动重发' WHEN `+pendingPauseSQL+` THEN '重启前已请求暂停，现已暂停；继续前会核对暂存与会话' ELSE error END,
 updated=? WHERE state IN ('hashing','uploading','submitting','finalizing')`, at)
	if err != nil {
		return err
	}

	return tx.Commit()
}
