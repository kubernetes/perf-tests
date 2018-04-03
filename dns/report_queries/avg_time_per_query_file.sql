SELECT
    runs.run_subid as subid,
    runs.query_file as source_file,
    ROUND(AVG(results.avg_latency), 4) as query_avg_latency
FROM runs
LEFT JOIN
    results on
        results.run_id = runs.run_id AND
        results.run_subid = runs.run_subid
GROUP BY (runs.run_subid)
