SELECT
    toJSONString(
            groupArray(
                    map(
                            'database', database,
                            'table', table,
                            'comment', table_comment,
                            'columns', columns
                    )
            )
    ) AS schema_json
FROM (
         SELECT
             c.database AS database,
             c.table AS table,
        t.comment AS table_comment,
        groupArray(
            map(
                'name', c.name,
                'type', c.type,
                'comment', concat(
                    c.comment,
                    if(c.is_in_primary_key, ' [PK]', ''),
                    if(c.is_in_sorting_key AND NOT c.is_in_primary_key, ' [SK]', ''),
                    if(c.is_in_partition_key, ' [PARTITION]', ''),
                    if(c.default_kind != '', concat(' ', c.default_kind, '=', c.default_expression), '')
                )
            )
        ) AS columns
         FROM system.columns AS c
             INNER JOIN system.tables AS t
         ON c.database = t.database AND c.table = t.name
         WHERE c.database NOT IN ('system', 'INFORMATION_SCHEMA', 'information_schema')
           AND t.engine NOT IN ('MaterializedView', 'View')
         -- AND c.database IN ({database_filter:Array(String)})
         GROUP BY c.database, c.table, t.comment
         ORDER BY c.database, c.table
     )
         FORMAT JSONEachRow;