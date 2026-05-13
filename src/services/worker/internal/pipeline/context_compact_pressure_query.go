//go:build !desktop

package pipeline

func latestContextCompactPressureAnchorSQL() string {
	return `SELECT re.data_json
	  FROM runs r
	  JOIN run_events re ON re.run_id = r.id
	   AND re.type = 'llm.turn.completed'
	 WHERE r.account_id = $1
	   AND r.thread_id = $2
	 ORDER BY re.ts DESC, re.seq DESC
	 LIMIT 10`
}

func compactConsecutiveFailuresSQL() string {
	return `SELECT re.data_json
	  FROM runs r
	  JOIN run_events re ON re.run_id = r.id
	   AND re.type = 'run.context_compact'
	 WHERE r.account_id = $1
	   AND r.thread_id = $2
	 ORDER BY re.ts DESC, re.seq DESC
	 LIMIT $3`
}
