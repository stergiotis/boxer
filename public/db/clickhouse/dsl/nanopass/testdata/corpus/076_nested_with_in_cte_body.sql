WITH outer_cte AS (WITH inner_cte AS (SELECT 1 AS v) SELECT v FROM inner_cte) SELECT v FROM outer_cte
