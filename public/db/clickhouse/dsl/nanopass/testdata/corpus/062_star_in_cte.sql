WITH cte AS (SELECT * FROM orders WHERE created > '2024-01-01')
SELECT * FROM cte
