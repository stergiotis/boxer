SELECT o.*, count(*) AS order_count
FROM orders AS o
GROUP BY o.id, o.amount, o.tenant_id, o.customer_id, o.created
