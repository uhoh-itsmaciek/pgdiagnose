package main

import (
	"encoding/json"
	"fmt"
	"github.com/jmoiron/sqlx"
	_ "github.com/lib/pq"
	"log"
)

type Check struct {
	Name    string      `json:"name"`
	Status  string      `json:"status"`
	Results interface{} `json:"results"`
}

func CheckSql(connstring string, plan Plan) ([]Check, error) {
	db, err := connectDB(connstring)
	if err != nil {
		return nil, err
	}
	defer db.Close()

	v := make([]Check, 8)
	v[0] = connCountCheck(db, plan.ConnectionLimit)
	v[1] = longQueriesCheck(db)
	v[2] = idleQueriesCheck(db)
	v[3] = unusedIndexesCheck(db)
	v[4] = bloatCheck(db)
	v[5] = hitRateCheck(db)
	v[6] = blockingCheck(db)
	v[7] = seqCheck(db)
	return v, nil
}

func PrettyJSON(whatever interface{}) (string, error) {
	js, err := json.MarshalIndent(whatever, "", "  ")
	return string(js), err
}

func connectDB(dbURL string) (*sqlx.DB, error) {
	db, err := sqlx.Open("postgres", dbURL)
	if err != nil {
		return nil, err
	}

	_, err = db.Exec("select 1")
	if err != nil {
		return nil, err
	}

	return db, nil
}

func makeErrorCheck(name string, err error) Check {
	log.Println(err)
	reason := make(map[string]string)
	reason["error"] = "could not do check"
	return Check{name, "skipped", reason}
}

type connCountResult struct {
	Count int64 `json:"count"`
}

func connCountCheck(db *sqlx.DB, limit int) Check {
	checkTitle := "Connection Count"
	var result []connCountResult
	err := db.Select(&result, "SELECT count(*) FROM pg_stat_activity where usename = current_user")
	if err != nil {
		return makeErrorCheck(checkTitle, err)
	}
	return Check{checkTitle, connCountStuats(result[0].Count, limit), result}
}

func connCountStuats(count int64, limit int) string {
	perc := float64(count) / float64(limit)
	switch {
	case perc >= 0.75 && perc < 0.9:
		return "yellow"
	case perc >= 0.9:
		return "red"
	}
	return "green"
}

type longQueriesResult struct {
	Pid      int64  `json:"pid"`
	Duration string `json:"duration"`
	Query    string `json:"query"`
}

func longQueriesCheck(db *sqlx.DB) Check {
	checkTitle := "Long Queries"
	var results []longQueriesResult
	err := db.Select(&results, longQueriesSQL)
	if err != nil {
		return makeErrorCheck(checkTitle, err)
	}
	return Check{checkTitle, longQueriesStatus(results), results}
}

func longQueriesStatus(results []longQueriesResult) string {
	if len(results) == 0 {
		return "green"
	} else {
		return "red"
	}
}

type idleQueriesResult struct {
	Pid      int64  `json:"pid"`
	Duration string `json:"duration"`
	Query    string `json:"query"`
}

func idleQueriesCheck(db *sqlx.DB) Check {
	checkTitle := "Idle in Transaction"
	var results []idleQueriesResult
	err := db.Select(&results, idleQueriesSQL)
	if err != nil {
		return makeErrorCheck(checkTitle, err)
	}
	return Check{checkTitle, idleQueriesStatus(results), results}
}

func idleQueriesStatus(results []idleQueriesResult) string {
	if len(results) == 0 {
		return "green"
	} else {
		return "red"
	}
}

type unusedIndexesResult struct {
	Reason          string `json:"reason"`
	Index           string `json:"index"`
	Index_scan_pct  string `json:"index_scan_pct"`
	Scans_per_write string `json:"scans_per_write"`
	Index_size      string `json:"index_size"`
	Table_size      string `json:"table_size"`
}

func unusedIndexesCheck(db *sqlx.DB) Check {
	checkTitle := "Indexes"
	var results []unusedIndexesResult
	err := db.Select(&results, unusedIndexesSQL)
	if err != nil {
		return makeErrorCheck(checkTitle, err)
	}
	return Check{checkTitle, unusedIndexesStatus(results), results}
}

func unusedIndexesStatus(results []unusedIndexesResult) string {
	if len(results) == 0 {
		return "green"
	} else {
		return "yellow"
	}
}

type bloatResult struct {
	Type   string `json:"type"`
	Object string `json:"object"`
	Bloat  int64  `json:"bloat"`
	Waste  string `json:"waste"`
}

func bloatCheck(db *sqlx.DB) Check {
	checkTitle := "Bloat"
	var results []bloatResult
	err := db.Select(&results, bloatSQL)
	if err != nil {
		return makeErrorCheck(checkTitle, err)
	}
	return Check{checkTitle, bloatStatus(results), results}
}

func bloatStatus(results []bloatResult) string {
	if len(results) == 0 {
		return "green"
	} else {
		return "red"
	}
}

type hitRateResult struct {
	Name  string  `json:"name"`
	Ratio float64 `json:"ratio"`
}

func hitRateCheck(db *sqlx.DB) Check {
	checkTitle := "Hit Rate"
	var results []hitRateResult
	err := db.Select(&results, hitRateSQL)
	if err != nil {
		return makeErrorCheck(checkTitle, err)
	}
	return Check{checkTitle, hitRateStatus(results), results}
}

func hitRateStatus(results []hitRateResult) string {
	if len(results) == 0 {
		return "green"
	} else {
		return "red"
	}
}

type blockingResult struct {
	Blocked_pid        int    `json:"blocked_pid"`
	Blocking_statement string `json:"blocking_statement"`
	Blocking_duration  string `json:"blocking_duration"`
	Blocking_pid       int    `json:"blocking_pid"`
	Blocked_statement  string `json:"blocked_statement"`
	Blocked_duration   string `json:"blocked_duration"`
}

func blockingCheck(db *sqlx.DB) Check {
	checkTitle := "Blocking Queries"
	var results []blockingResult
	err := db.Select(&results, blockingSQL)
	if err != nil {
		return makeErrorCheck(checkTitle, err)
	}
	return Check{checkTitle, blockingStatus(results), results}
}

func blockingStatus(results []blockingResult) string {
	if len(results) == 0 {
		return "green"
	} else {
		return "red"
	}
}

type sequenceResult struct {
	Col string  `json:"column"`
	Seq string  `json:"sequence"`
	Pct float64 `json:"percent_used"`
}

func seqCheck(db *sqlx.DB) Check {
	yellowCutoff := 75.0
	checkTitle := "Sequences"
	var tmpSeqs []sequenceResult
	var retSeqs []sequenceResult

	err := db.Select(&tmpSeqs, seqsOnInt4SQL)
	if err != nil {
		return makeErrorCheck(checkTitle, err)
	}

	sql := `select round((last_value::float / pow(2, 31))::numeric * 100, 2) as pct from %s`
	maxPct := 0.0
	for _, seq := range tmpSeqs {
		err = db.Get(&seq, fmt.Sprintf(sql, seq.Seq))
		if err != nil {
			log.Printf(err.Error())
		}
		if seq.Pct > yellowCutoff {
			retSeqs = append(retSeqs, seq)
			if seq.Pct > maxPct {
				maxPct = seq.Pct
			}
		}
	}

	var status string
	if maxPct >= 90.0 {
		status = "red"
	} else if maxPct >= yellowCutoff {
		status = "yellow"
	} else {
		status = "green"
	}

	return Check{checkTitle, status, retSeqs}
}

const (
	longQueriesSQL = `
	  SELECT pid, now()-query_start as duration, query
	  FROM pg_stat_activity
	  WHERE now()-query_start > '1 minute'::interval
		AND state = 'active'
		;`

	idleQueriesSQL = `
	  SELECT pid, now()-query_start as duration, query
	  FROM pg_stat_activity
	  WHERE now()-query_start > '1 minute'::interval
		AND state like 'idle in trans%'
		;`

	// http://www.databasesoup.com/2014/05/new-finding-unused-indexes-query.html
	unusedIndexesSQL = `
WITH table_scans as (
    SELECT relid,
        tables.idx_scan + tables.seq_scan as all_scans,
        ( tables.n_tup_ins + tables.n_tup_upd + tables.n_tup_del ) as writes,
                pg_relation_size(relid) as table_size
        FROM pg_stat_user_tables as tables
),
all_writes as (
    SELECT sum(writes) as total_writes
    FROM table_scans
),
indexes as (
    SELECT idx_stat.relid, idx_stat.indexrelid,
        idx_stat.schemaname, idx_stat.relname as tablename,
        idx_stat.indexrelname as indexname,
        idx_stat.idx_scan,
        pg_relation_size(idx_stat.indexrelid) as index_bytes,
        indexdef ~* 'USING btree' AS idx_is_btree
    FROM pg_stat_user_indexes as idx_stat
        JOIN pg_index
            USING (indexrelid)
        JOIN pg_indexes as indexes
            ON idx_stat.schemaname = indexes.schemaname
                AND idx_stat.relname = indexes.tablename
                AND idx_stat.indexrelname = indexes.indexname
    WHERE pg_index.indisunique = FALSE
),
index_ratios AS (
	SELECT schemaname || '.' || tablename || '::' || indexname as index,
    idx_scan, all_scans,
    round(( CASE WHEN all_scans = 0 THEN 0.0::NUMERIC
        ELSE idx_scan::NUMERIC/all_scans * 100 END),2) as index_scan_pct,
    writes,
    round((CASE WHEN writes = 0 THEN idx_scan::NUMERIC ELSE idx_scan::NUMERIC/writes END),2)
        as scans_per_write,
    pg_size_pretty(index_bytes) as index_size,
    pg_size_pretty(table_size) as table_size,
    idx_is_btree, index_bytes
    FROM indexes
    JOIN table_scans
    USING (relid)
		WHERE index_bytes > 64*1024*1024 AND table_size > 64*1024*1024
),
index_groups AS (
SELECT 'Never Used Indexes' as reason, *, 1 as grp
FROM index_ratios
WHERE
    idx_scan = 0
    and idx_is_btree
UNION ALL
SELECT 'Low Scans, High Writes' as reason, *, 2 as grp
FROM index_ratios
WHERE
    scans_per_write <= 1
    and index_scan_pct < 10
    and idx_scan > 0
    and writes > 100
    and idx_is_btree
UNION ALL
SELECT 'Seldom Used Large Indexes' as reason, *, 3 as grp
FROM index_ratios
WHERE
    index_scan_pct < 5
    and scans_per_write > 1
    and idx_scan > 0
    and idx_is_btree
    and index_bytes > 100000000
)

SELECT reason, index,
  index_scan_pct, scans_per_write, index_size, table_size
FROM index_groups;
`
	bloatSQL = `
WITH constants AS (
  SELECT current_setting('block_size')::numeric AS bs, 23 AS hdr, 4 AS ma
), bloat_info AS (
  SELECT
    ma,bs,schemaname,tablename,
    (datawidth+(hdr+ma-(case when hdr%ma=0 THEN ma ELSE hdr%ma END)))::numeric AS datahdr,
    (maxfracsum*(nullhdr+ma-(case when nullhdr%ma=0 THEN ma ELSE nullhdr%ma END))) AS nullhdr2
  FROM (
    SELECT
      schemaname, tablename, hdr, ma, bs,
      SUM((1-null_frac)*avg_width) AS datawidth,
      MAX(null_frac) AS maxfracsum,
      hdr+(
        SELECT 1+count(*)/8
        FROM pg_stats s2
        WHERE null_frac<>0 AND s2.schemaname = s.schemaname AND s2.tablename = s.tablename
      ) AS nullhdr
    FROM pg_stats s, constants
    GROUP BY 1,2,3,4,5
  ) AS foo
), table_bloat AS (
  SELECT
    schemaname, tablename, cc.relpages, bs,
    CEIL((cc.reltuples*((datahdr+ma-
      (CASE WHEN datahdr%ma=0 THEN ma ELSE datahdr%ma END))+nullhdr2+4))/(bs-20::float)) AS otta
  FROM bloat_info
  JOIN pg_class cc ON cc.relname = bloat_info.tablename
  JOIN pg_namespace nn ON cc.relnamespace = nn.oid AND nn.nspname = bloat_info.schemaname AND nn.nspname <> 'information_schema'
), index_bloat AS (
  SELECT
    schemaname, tablename, bs,
    COALESCE(c2.relname,'?') AS iname, COALESCE(c2.reltuples,0) AS ituples, COALESCE(c2.relpages,0) AS ipages,
    COALESCE(CEIL((c2.reltuples*(datahdr-12))/(bs-20::float)),0) AS iotta
  FROM bloat_info
  JOIN pg_class cc ON cc.relname = bloat_info.tablename
  JOIN pg_namespace nn ON cc.relnamespace = nn.oid AND nn.nspname = bloat_info.schemaname AND nn.nspname <> 'information_schema'
  JOIN pg_index i ON indrelid = cc.oid
  JOIN pg_class c2 ON c2.oid = i.indexrelid
)
SELECT
	type, object, bloat::int, pg_size_pretty(raw_waste) as waste
FROM
(SELECT
  'table' as type,
  schemaname ||'.'|| tablename as object,
  ROUND(CASE WHEN otta=0 THEN 0.0 ELSE table_bloat.relpages/otta::numeric END,1) AS bloat,
  CASE WHEN relpages < otta THEN '0' ELSE (bs*(table_bloat.relpages-otta)::bigint)::bigint END AS raw_waste
FROM
  table_bloat
    UNION
SELECT
  'index' as type,
  schemaname || '.' || tablename || '::' || iname as object,
  ROUND(CASE WHEN iotta=0 OR ipages=0 THEN 0.0 ELSE ipages/iotta::numeric END,1) AS bloat,
  CASE WHEN ipages < iotta THEN '0' ELSE (bs*(ipages-iotta))::bigint END AS raw_waste
FROM
  index_bloat) bloat_summary
WHERE raw_waste > 64*1024*1024 AND bloat > 10
ORDER BY raw_waste DESC, bloat DESC
;`
	hitRateSQL = `
WITH overall_rates AS (
  SELECT
    'overall index hit rate' AS name,
    sum(idx_blks_hit) / nullif(sum(idx_blks_hit + idx_blks_read), 0) AS ratio
  FROM pg_statio_user_indexes
  UNION ALL
  SELECT
    'overall cache hit rate' AS name,
    sum(heap_blks_hit) / nullif(sum(heap_blks_hit) + sum(heap_blks_read), 0) AS ratio
  FROM pg_statio_user_tables
)
, table_rates AS (
  SELECT
    schemaname || '.' || relname AS name,
    idx_scan::float/(seq_scan+idx_scan) as ratio
  FROM pg_stat_user_tables
  WHERE
     pg_total_relation_size(relid) > 64*1024*1024
     AND idx_scan > 0
)
, combined AS (
  SELECT * FROM overall_rates
  UNION ALL
  SELECT * FROM table_rates
)

SELECT * FROM combined WHERE ratio < 0.99
;`

	blockingSQL = `
  SELECT bl.pid AS blocked_pid,
    ka.query AS blocking_statement,
    now() - ka.query_start AS blocking_duration,
    kl.pid AS blocking_pid,
    a.query AS blocked_statement,
    now() - a.query_start AS blocked_duration
  FROM pg_catalog.pg_locks bl
  JOIN pg_catalog.pg_stat_activity a
    ON bl.pid = a.pid
  JOIN pg_catalog.pg_locks kl
    JOIN pg_catalog.pg_stat_activity ka
      ON kl.pid = ka.pid
  ON bl.transactionid = kl.transactionid AND bl.pid != kl.pid
  WHERE NOT bl.granted
			;`

	seqsOnInt4SQL = `
	SELECT ns.nspname||'.'||c.relname||'('||attname||')' as col, ns.nspname||'.'||s.relname as seq
	FROM pg_attribute a
	INNER JOIN pg_attrdef d ON a.attrelid = d.adrelid AND a.attnum = d.adnum
	INNER JOIN pg_class s ON s.relkind = 'S' AND s.relname = regexp_replace(d.adsrc, $s$nextval\('(.*)'::regclass\)$$s$, '\1')
	INNER JOIN pg_class c ON c.oid = a.attrelid
	INNER JOIN pg_namespace ns ON ns.oid = c.relnamespace
	WHERE atttypid = 'int4'::regtype
	;`
)
