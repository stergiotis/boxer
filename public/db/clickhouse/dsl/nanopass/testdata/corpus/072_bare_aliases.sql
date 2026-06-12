SELECT o.amount, c.name FROM orders o JOIN customers c ON o.cid = c.id WHERE o.amount > 10
