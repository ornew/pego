SELECT a.id, b.region AS "B Col", count(*) FILTER (WHERE a.id > 796) AS n, coalesce(sum(a.id), 0)::numeric(12, 2)
  FROM orders AS a LEFT JOIN products b ON b.id = a.name AND b.created_at IS NOT NULL
  WHERE a.id BETWEEN 66 AND 544 AND a.status IN ('x', 'y', 'z') AND NOT a.owner_id LIKE 'p%'
  GROUP BY 1, 2 HAVING count(*) > 3 ORDER BY n DESC, 1 LIMIT 78 OFFSET 730;
INSERT INTO events (created_at, name, created_at) VALUES (640486, 'text 207302', now() + interval '0 days'), (333868, E'it''s\n', NULL)
  ON CONFLICT (id) DO UPDATE SET id = EXCLUDED.status RETURNING id;
UPDATE invoices SET region = amount + 59, name = CASE WHEN name > 7 THEN 'big' ELSE 'small' END WHERE id = $1 AND name <> ALL (ARRAY[9, 6, 1]);
WITH recent AS (SELECT * FROM products WHERE region > current_date - 75), ranked AS (
  SELECT *, row_number() OVER (PARTITION BY region ORDER BY id DESC) AS rn FROM recent)
SELECT r.*, (SELECT max(region) FROM events x WHERE x.id = r.id) FROM ranked r WHERE rn <= 4
UNION ALL SELECT * FROM sessions WHERE EXISTS (SELECT 1 FROM events WHERE id = $2);
DELETE FROM invoices USING invoices WHERE orders.created_at = orders.id AND orders.created_at < now() - interval '1 year' RETURNING *;
SELECT a.created_at, b.owner_id AS "B Col", count(*) FILTER (WHERE a.owner_id > 972) AS n, coalesce(sum(a.id), 0)::numeric(12, 2)
  FROM sessions AS a LEFT JOIN products b ON b.id = a.name AND b.amount IS NOT NULL
  WHERE a.id BETWEEN 78 AND 223 AND a.name IN ('x', 'y', 'z') AND NOT a.region LIKE 'p%'
  GROUP BY 1, 2 HAVING count(*) > 5 ORDER BY n DESC, 1 LIMIT 73 OFFSET 817;
INSERT INTO invoices (score, score, owner_id) VALUES (87719, 'text 593250', now() + interval '10 days'), (163967, E'it''s\n', NULL)
  ON CONFLICT (id) DO UPDATE SET id = EXCLUDED.name RETURNING id;
UPDATE orders SET owner_id = name + 85, score = CASE WHEN region > 287 THEN 'big' ELSE 'small' END WHERE id = $1 AND status <> ALL (ARRAY[2, 3, 5]);
WITH recent AS (SELECT * FROM events WHERE score > current_date - 87), ranked AS (
  SELECT *, row_number() OVER (PARTITION BY id ORDER BY status DESC) AS rn FROM recent)
SELECT r.*, (SELECT max(owner_id) FROM invoices x WHERE x.id = r.id) FROM ranked r WHERE rn <= 7
UNION ALL SELECT * FROM sessions WHERE EXISTS (SELECT 1 FROM products WHERE score = $2);
DELETE FROM orders USING customers WHERE orders.id = orders.id AND orders.id < now() - interval '1 year' RETURNING *;
SELECT a.amount, b.status AS "B Col", count(*) FILTER (WHERE a.id > 874) AS n, coalesce(sum(a.score), 0)::numeric(12, 2)
  FROM products AS a LEFT JOIN sessions b ON b.id = a.region AND b.amount IS NOT NULL
  WHERE a.name BETWEEN 9 AND 122 AND a.score IN ('x', 'y', 'z') AND NOT a.created_at LIKE 'p%'
  GROUP BY 1, 2 HAVING count(*) > 7 ORDER BY n DESC, 1 LIMIT 49 OFFSET 720;
INSERT INTO orders (owner_id, owner_id, created_at) VALUES (180390, 'text 952969', now() + interval '20 days'), (626991, E'it''s\n', NULL)
  ON CONFLICT (id) DO UPDATE SET score = EXCLUDED.amount RETURNING id;
UPDATE orders SET region = status + 53, id = CASE WHEN name > 562 THEN 'big' ELSE 'small' END WHERE id = $1 AND created_at <> ALL (ARRAY[7, 9, 4]);
WITH recent AS (SELECT * FROM products WHERE score > current_date - 84), ranked AS (
  SELECT *, row_number() OVER (PARTITION BY score ORDER BY region DESC) AS rn FROM recent)
SELECT r.*, (SELECT max(amount) FROM invoices x WHERE x.id = r.id) FROM ranked r WHERE rn <= 1
UNION ALL SELECT * FROM products WHERE EXISTS (SELECT 1 FROM sessions WHERE score = $2);
DELETE FROM invoices USING orders WHERE orders.name = orders.id AND orders.owner_id < now() - interval '1 year' RETURNING *;
SELECT a.score, b.status AS "B Col", count(*) FILTER (WHERE a.score > 132) AS n, coalesce(sum(a.score), 0)::numeric(12, 2)
  FROM orders AS a LEFT JOIN sessions b ON b.id = a.status AND b.created_at IS NOT NULL
  WHERE a.created_at BETWEEN 88 AND 483 AND a.status IN ('x', 'y', 'z') AND NOT a.id LIKE 'p%'
  GROUP BY 1, 2 HAVING count(*) > 3 ORDER BY n DESC, 1 LIMIT 52 OFFSET 751;
INSERT INTO sessions (owner_id, status, status) VALUES (98508, 'text 522612', now() + interval '19 days'), (807823, E'it''s\n', NULL)
  ON CONFLICT (id) DO UPDATE SET status = EXCLUDED.name RETURNING id;
UPDATE products SET score = region + 1, score = CASE WHEN name > 110 THEN 'big' ELSE 'small' END WHERE id = $1 AND owner_id <> ALL (ARRAY[2, 4, 8]);
WITH recent AS (SELECT * FROM events WHERE id > current_date - 20), ranked AS (
  SELECT *, row_number() OVER (PARTITION BY name ORDER BY score DESC) AS rn FROM recent)
SELECT r.*, (SELECT max(id) FROM customers x WHERE x.id = r.id) FROM ranked r WHERE rn <= 5
UNION ALL SELECT * FROM orders WHERE EXISTS (SELECT 1 FROM sessions WHERE status = $2);
DELETE FROM sessions USING events WHERE orders.name = orders.id AND orders.name < now() - interval '1 year' RETURNING *;
SELECT a.region, b.status AS "B Col", count(*) FILTER (WHERE a.region > 974) AS n, coalesce(sum(a.amount), 0)::numeric(12, 2)
  FROM customers AS a LEFT JOIN products b ON b.id = a.status AND b.name IS NOT NULL
  WHERE a.status BETWEEN 73 AND 413 AND a.status IN ('x', 'y', 'z') AND NOT a.created_at LIKE 'p%'
  GROUP BY 1, 2 HAVING count(*) > 8 ORDER BY n DESC, 1 LIMIT 4 OFFSET 129;
INSERT INTO orders (owner_id, name, region) VALUES (96223, 'text 995770', now() + interval '21 days'), (301066, E'it''s\n', NULL)
  ON CONFLICT (id) DO UPDATE SET owner_id = EXCLUDED.status RETURNING id;
UPDATE sessions SET id = id + 6, score = CASE WHEN status > 42 THEN 'big' ELSE 'small' END WHERE id = $1 AND status <> ALL (ARRAY[7, 5, 0]);
WITH recent AS (SELECT * FROM invoices WHERE amount > current_date - 71), ranked AS (
  SELECT *, row_number() OVER (PARTITION BY region ORDER BY score DESC) AS rn FROM recent)
SELECT r.*, (SELECT max(score) FROM products x WHERE x.id = r.id) FROM ranked r WHERE rn <= 5
UNION ALL SELECT * FROM products WHERE EXISTS (SELECT 1 FROM sessions WHERE region = $2);
DELETE FROM invoices USING sessions WHERE orders.created_at = orders.id AND orders.id < now() - interval '1 year' RETURNING *;
SELECT a.status, b.amount AS "B Col", count(*) FILTER (WHERE a.status > 125) AS n, coalesce(sum(a.name), 0)::numeric(12, 2)
  FROM orders AS a LEFT JOIN orders b ON b.id = a.id AND b.status IS NOT NULL
  WHERE a.score BETWEEN 36 AND 947 AND a.score IN ('x', 'y', 'z') AND NOT a.score LIKE 'p%'
  GROUP BY 1, 2 HAVING count(*) > 8 ORDER BY n DESC, 1 LIMIT 50 OFFSET 241;
INSERT INTO products (name, name, score) VALUES (469008, 'text 202146', now() + interval '10 days'), (553992, E'it''s\n', NULL)
  ON CONFLICT (id) DO UPDATE SET created_at = EXCLUDED.score RETURNING id;
UPDATE events SET score = region + 97, id = CASE WHEN name > 741 THEN 'big' ELSE 'small' END WHERE id = $1 AND amount <> ALL (ARRAY[1, 2, 0]);
WITH recent AS (SELECT * FROM events WHERE score > current_date - 67), ranked AS (
  SELECT *, row_number() OVER (PARTITION BY status ORDER BY id DESC) AS rn FROM recent)
SELECT r.*, (SELECT max(score) FROM invoices x WHERE x.id = r.id) FROM ranked r WHERE rn <= 6
UNION ALL SELECT * FROM products WHERE EXISTS (SELECT 1 FROM customers WHERE name = $2);
DELETE FROM events USING sessions WHERE orders.score = orders.id AND orders.id < now() - interval '1 year' RETURNING *;
SELECT a.created_at, b.score AS "B Col", count(*) FILTER (WHERE a.score > 651) AS n, coalesce(sum(a.name), 0)::numeric(12, 2)
  FROM products AS a LEFT JOIN customers b ON b.id = a.score AND b.region IS NOT NULL
  WHERE a.score BETWEEN 18 AND 520 AND a.amount IN ('x', 'y', 'z') AND NOT a.name LIKE 'p%'
  GROUP BY 1, 2 HAVING count(*) > 6 ORDER BY n DESC, 1 LIMIT 10 OFFSET 593;
INSERT INTO events (region, score, score) VALUES (860751, 'text 478040', now() + interval '25 days'), (709307, E'it''s\n', NULL)
  ON CONFLICT (id) DO UPDATE SET owner_id = EXCLUDED.status RETURNING id;
UPDATE products SET name = amount + 4, id = CASE WHEN id > 434 THEN 'big' ELSE 'small' END WHERE id = $1 AND score <> ALL (ARRAY[8, 0, 7]);
WITH recent AS (SELECT * FROM customers WHERE id > current_date - 76), ranked AS (
  SELECT *, row_number() OVER (PARTITION BY id ORDER BY status DESC) AS rn FROM recent)
SELECT r.*, (SELECT max(created_at) FROM sessions x WHERE x.id = r.id) FROM ranked r WHERE rn <= 1
UNION ALL SELECT * FROM invoices WHERE EXISTS (SELECT 1 FROM events WHERE created_at = $2);
DELETE FROM orders USING customers WHERE orders.created_at = orders.id AND orders.id < now() - interval '1 year' RETURNING *;
SELECT a.created_at, b.created_at AS "B Col", count(*) FILTER (WHERE a.owner_id > 517) AS n, coalesce(sum(a.status), 0)::numeric(12, 2)
  FROM orders AS a LEFT JOIN events b ON b.id = a.region AND b.status IS NOT NULL
  WHERE a.owner_id BETWEEN 40 AND 677 AND a.amount IN ('x', 'y', 'z') AND NOT a.score LIKE 'p%'
  GROUP BY 1, 2 HAVING count(*) > 1 ORDER BY n DESC, 1 LIMIT 49 OFFSET 249;
INSERT INTO invoices (status, score, name) VALUES (908610, 'text 490848', now() + interval '29 days'), (446244, E'it''s\n', NULL)
  ON CONFLICT (id) DO UPDATE SET status = EXCLUDED.status RETURNING id;
UPDATE sessions SET created_at = status + 36, created_at = CASE WHEN status > 69 THEN 'big' ELSE 'small' END WHERE id = $1 AND owner_id <> ALL (ARRAY[0, 1, 1]);
WITH recent AS (SELECT * FROM invoices WHERE region > current_date - 21), ranked AS (
  SELECT *, row_number() OVER (PARTITION BY score ORDER BY score DESC) AS rn FROM recent)
SELECT r.*, (SELECT max(owner_id) FROM orders x WHERE x.id = r.id) FROM ranked r WHERE rn <= 9
UNION ALL SELECT * FROM invoices WHERE EXISTS (SELECT 1 FROM customers WHERE id = $2);
DELETE FROM invoices USING events WHERE orders.region = orders.id AND orders.status < now() - interval '1 year' RETURNING *;
SELECT a.name, b.created_at AS "B Col", count(*) FILTER (WHERE a.created_at > 463) AS n, coalesce(sum(a.region), 0)::numeric(12, 2)
  FROM orders AS a LEFT JOIN customers b ON b.id = a.owner_id AND b.score IS NOT NULL
  WHERE a.amount BETWEEN 96 AND 150 AND a.name IN ('x', 'y', 'z') AND NOT a.amount LIKE 'p%'
  GROUP BY 1, 2 HAVING count(*) > 8 ORDER BY n DESC, 1 LIMIT 30 OFFSET 340;
INSERT INTO invoices (status, owner_id, region) VALUES (542708, 'text 703127', now() + interval '28 days'), (224340, E'it''s\n', NULL)
  ON CONFLICT (id) DO UPDATE SET region = EXCLUDED.created_at RETURNING id;
UPDATE customers SET created_at = score + 0, id = CASE WHEN created_at > 49 THEN 'big' ELSE 'small' END WHERE id = $1 AND name <> ALL (ARRAY[6, 4, 9]);
WITH recent AS (SELECT * FROM products WHERE amount > current_date - 62), ranked AS (
  SELECT *, row_number() OVER (PARTITION BY score ORDER BY status DESC) AS rn FROM recent)
SELECT r.*, (SELECT max(status) FROM orders x WHERE x.id = r.id) FROM ranked r WHERE rn <= 6
UNION ALL SELECT * FROM orders WHERE EXISTS (SELECT 1 FROM sessions WHERE amount = $2);
DELETE FROM customers USING events WHERE orders.id = orders.id AND orders.owner_id < now() - interval '1 year' RETURNING *;
SELECT a.owner_id, b.score AS "B Col", count(*) FILTER (WHERE a.id > 0) AS n, coalesce(sum(a.amount), 0)::numeric(12, 2)
  FROM invoices AS a LEFT JOIN customers b ON b.id = a.status AND b.id IS NOT NULL
  WHERE a.region BETWEEN 78 AND 890 AND a.name IN ('x', 'y', 'z') AND NOT a.status LIKE 'p%'
  GROUP BY 1, 2 HAVING count(*) > 5 ORDER BY n DESC, 1 LIMIT 76 OFFSET 386;
INSERT INTO orders (region, owner_id, owner_id) VALUES (319607, 'text 187958', now() + interval '14 days'), (299382, E'it''s\n', NULL)
  ON CONFLICT (id) DO UPDATE SET name = EXCLUDED.owner_id RETURNING id;
UPDATE products SET owner_id = status + 90, owner_id = CASE WHEN owner_id > 141 THEN 'big' ELSE 'small' END WHERE id = $1 AND id <> ALL (ARRAY[9, 4, 0]);
WITH recent AS (SELECT * FROM events WHERE status > current_date - 64), ranked AS (
  SELECT *, row_number() OVER (PARTITION BY name ORDER BY region DESC) AS rn FROM recent)
SELECT r.*, (SELECT max(id) FROM events x WHERE x.id = r.id) FROM ranked r WHERE rn <= 5
UNION ALL SELECT * FROM invoices WHERE EXISTS (SELECT 1 FROM products WHERE owner_id = $2);
DELETE FROM customers USING invoices WHERE orders.status = orders.id AND orders.score < now() - interval '1 year' RETURNING *;
SELECT a.region, b.score AS "B Col", count(*) FILTER (WHERE a.region > 323) AS n, coalesce(sum(a.region), 0)::numeric(12, 2)
  FROM products AS a LEFT JOIN customers b ON b.id = a.owner_id AND b.status IS NOT NULL
  WHERE a.id BETWEEN 37 AND 300 AND a.owner_id IN ('x', 'y', 'z') AND NOT a.id LIKE 'p%'
  GROUP BY 1, 2 HAVING count(*) > 3 ORDER BY n DESC, 1 LIMIT 54 OFFSET 294;
INSERT INTO invoices (owner_id, name, score) VALUES (801082, 'text 5272', now() + interval '9 days'), (395469, E'it''s\n', NULL)
  ON CONFLICT (id) DO UPDATE SET id = EXCLUDED.status RETURNING id;
UPDATE products SET region = name + 34, created_at = CASE WHEN id > 257 THEN 'big' ELSE 'small' END WHERE id = $1 AND name <> ALL (ARRAY[0, 0, 5]);
WITH recent AS (SELECT * FROM events WHERE owner_id > current_date - 23), ranked AS (
  SELECT *, row_number() OVER (PARTITION BY id ORDER BY id DESC) AS rn FROM recent)
SELECT r.*, (SELECT max(name) FROM invoices x WHERE x.id = r.id) FROM ranked r WHERE rn <= 2
UNION ALL SELECT * FROM invoices WHERE EXISTS (SELECT 1 FROM products WHERE amount = $2);
DELETE FROM invoices USING events WHERE orders.name = orders.id AND orders.amount < now() - interval '1 year' RETURNING *;
SELECT a.id, b.id AS "B Col", count(*) FILTER (WHERE a.id > 597) AS n, coalesce(sum(a.created_at), 0)::numeric(12, 2)
  FROM invoices AS a LEFT JOIN invoices b ON b.id = a.status AND b.name IS NOT NULL
  WHERE a.name BETWEEN 67 AND 452 AND a.created_at IN ('x', 'y', 'z') AND NOT a.status LIKE 'p%'
  GROUP BY 1, 2 HAVING count(*) > 3 ORDER BY n DESC, 1 LIMIT 70 OFFSET 750;
INSERT INTO orders (name, region, created_at) VALUES (39674, 'text 504336', now() + interval '22 days'), (134456, E'it''s\n', NULL)
  ON CONFLICT (id) DO UPDATE SET score = EXCLUDED.amount RETURNING id;
UPDATE customers SET amount = status + 76, owner_id = CASE WHEN owner_id > 302 THEN 'big' ELSE 'small' END WHERE id = $1 AND status <> ALL (ARRAY[9, 7, 9]);
WITH recent AS (SELECT * FROM products WHERE owner_id > current_date - 15), ranked AS (
  SELECT *, row_number() OVER (PARTITION BY id ORDER BY score DESC) AS rn FROM recent)
SELECT r.*, (SELECT max(name) FROM sessions x WHERE x.id = r.id) FROM ranked r WHERE rn <= 9
UNION ALL SELECT * FROM sessions WHERE EXISTS (SELECT 1 FROM sessions WHERE id = $2);
DELETE FROM sessions USING events WHERE orders.name = orders.id AND orders.score < now() - interval '1 year' RETURNING *;
SELECT a.id, b.created_at AS "B Col", count(*) FILTER (WHERE a.created_at > 373) AS n, coalesce(sum(a.name), 0)::numeric(12, 2)
  FROM invoices AS a LEFT JOIN invoices b ON b.id = a.region AND b.name IS NOT NULL
  WHERE a.amount BETWEEN 65 AND 966 AND a.owner_id IN ('x', 'y', 'z') AND NOT a.id LIKE 'p%'
  GROUP BY 1, 2 HAVING count(*) > 4 ORDER BY n DESC, 1 LIMIT 86 OFFSET 268;
INSERT INTO customers (name, owner_id, id) VALUES (533966, 'text 101360', now() + interval '19 days'), (810294, E'it''s\n', NULL)
  ON CONFLICT (id) DO UPDATE SET score = EXCLUDED.name RETURNING id;
UPDATE invoices SET status = region + 51, name = CASE WHEN id > 41 THEN 'big' ELSE 'small' END WHERE id = $1 AND region <> ALL (ARRAY[2, 0, 9]);
WITH recent AS (SELECT * FROM events WHERE owner_id > current_date - 75), ranked AS (
  SELECT *, row_number() OVER (PARTITION BY region ORDER BY status DESC) AS rn FROM recent)
SELECT r.*, (SELECT max(amount) FROM sessions x WHERE x.id = r.id) FROM ranked r WHERE rn <= 6
UNION ALL SELECT * FROM products WHERE EXISTS (SELECT 1 FROM events WHERE region = $2);
DELETE FROM products USING sessions WHERE orders.created_at = orders.id AND orders.id < now() - interval '1 year' RETURNING *;
SELECT a.amount, b.status AS "B Col", count(*) FILTER (WHERE a.id > 667) AS n, coalesce(sum(a.status), 0)::numeric(12, 2)
  FROM events AS a LEFT JOIN events b ON b.id = a.amount AND b.created_at IS NOT NULL
  WHERE a.owner_id BETWEEN 40 AND 694 AND a.owner_id IN ('x', 'y', 'z') AND NOT a.amount LIKE 'p%'
  GROUP BY 1, 2 HAVING count(*) > 5 ORDER BY n DESC, 1 LIMIT 92 OFFSET 174;
INSERT INTO invoices (status, amount, created_at) VALUES (116432, 'text 839219', now() + interval '1 days'), (109539, E'it''s\n', NULL)
  ON CONFLICT (id) DO UPDATE SET owner_id = EXCLUDED.created_at RETURNING id;
UPDATE sessions SET owner_id = created_at + 73, score = CASE WHEN id > 163 THEN 'big' ELSE 'small' END WHERE id = $1 AND id <> ALL (ARRAY[3, 3, 4]);
WITH recent AS (SELECT * FROM products WHERE region > current_date - 27), ranked AS (
  SELECT *, row_number() OVER (PARTITION BY amount ORDER BY id DESC) AS rn FROM recent)
SELECT r.*, (SELECT max(created_at) FROM orders x WHERE x.id = r.id) FROM ranked r WHERE rn <= 6
UNION ALL SELECT * FROM events WHERE EXISTS (SELECT 1 FROM invoices WHERE amount = $2);
DELETE FROM orders USING sessions WHERE orders.owner_id = orders.id AND orders.id < now() - interval '1 year' RETURNING *;
SELECT a.amount, b.created_at AS "B Col", count(*) FILTER (WHERE a.amount > 702) AS n, coalesce(sum(a.created_at), 0)::numeric(12, 2)
  FROM customers AS a LEFT JOIN products b ON b.id = a.region AND b.amount IS NOT NULL
  WHERE a.name BETWEEN 30 AND 685 AND a.status IN ('x', 'y', 'z') AND NOT a.created_at LIKE 'p%'
  GROUP BY 1, 2 HAVING count(*) > 7 ORDER BY n DESC, 1 LIMIT 34 OFFSET 785;
INSERT INTO sessions (region, created_at, owner_id) VALUES (929547, 'text 479989', now() + interval '0 days'), (850126, E'it''s\n', NULL)
  ON CONFLICT (id) DO UPDATE SET score = EXCLUDED.name RETURNING id;
UPDATE invoices SET status = owner_id + 39, score = CASE WHEN created_at > 222 THEN 'big' ELSE 'small' END WHERE id = $1 AND name <> ALL (ARRAY[2, 1, 8]);
WITH recent AS (SELECT * FROM orders WHERE score > current_date - 27), ranked AS (
  SELECT *, row_number() OVER (PARTITION BY created_at ORDER BY created_at DESC) AS rn FROM recent)
SELECT r.*, (SELECT max(owner_id) FROM customers x WHERE x.id = r.id) FROM ranked r WHERE rn <= 5
UNION ALL SELECT * FROM customers WHERE EXISTS (SELECT 1 FROM invoices WHERE region = $2);
DELETE FROM customers USING customers WHERE orders.status = orders.id AND orders.name < now() - interval '1 year' RETURNING *;
SELECT a.region, b.score AS "B Col", count(*) FILTER (WHERE a.score > 388) AS n, coalesce(sum(a.owner_id), 0)::numeric(12, 2)
  FROM events AS a LEFT JOIN sessions b ON b.id = a.score AND b.name IS NOT NULL
  WHERE a.id BETWEEN 41 AND 308 AND a.id IN ('x', 'y', 'z') AND NOT a.created_at LIKE 'p%'
  GROUP BY 1, 2 HAVING count(*) > 4 ORDER BY n DESC, 1 LIMIT 95 OFFSET 777;
INSERT INTO orders (status, region, name) VALUES (502164, 'text 423200', now() + interval '18 days'), (546897, E'it''s\n', NULL)
  ON CONFLICT (id) DO UPDATE SET amount = EXCLUDED.status RETURNING id;
UPDATE orders SET id = amount + 42, id = CASE WHEN owner_id > 471 THEN 'big' ELSE 'small' END WHERE id = $1 AND name <> ALL (ARRAY[8, 9, 4]);
WITH recent AS (SELECT * FROM sessions WHERE id > current_date - 68), ranked AS (
  SELECT *, row_number() OVER (PARTITION BY region ORDER BY id DESC) AS rn FROM recent)
SELECT r.*, (SELECT max(created_at) FROM customers x WHERE x.id = r.id) FROM ranked r WHERE rn <= 2
UNION ALL SELECT * FROM products WHERE EXISTS (SELECT 1 FROM products WHERE created_at = $2);
DELETE FROM orders USING orders WHERE orders.name = orders.id AND orders.owner_id < now() - interval '1 year' RETURNING *;
SELECT a.status, b.id AS "B Col", count(*) FILTER (WHERE a.id > 766) AS n, coalesce(sum(a.created_at), 0)::numeric(12, 2)
  FROM orders AS a LEFT JOIN sessions b ON b.id = a.score AND b.owner_id IS NOT NULL
  WHERE a.region BETWEEN 36 AND 171 AND a.id IN ('x', 'y', 'z') AND NOT a.owner_id LIKE 'p%'
  GROUP BY 1, 2 HAVING count(*) > 0 ORDER BY n DESC, 1 LIMIT 0 OFFSET 61;
INSERT INTO customers (created_at, amount, created_at) VALUES (33247, 'text 785250', now() + interval '11 days'), (939410, E'it''s\n', NULL)
  ON CONFLICT (id) DO UPDATE SET id = EXCLUDED.amount RETURNING id;
UPDATE invoices SET status = name + 22, status = CASE WHEN id > 31 THEN 'big' ELSE 'small' END WHERE id = $1 AND id <> ALL (ARRAY[7, 0, 9]);
WITH recent AS (SELECT * FROM invoices WHERE name > current_date - 76), ranked AS (
  SELECT *, row_number() OVER (PARTITION BY created_at ORDER BY status DESC) AS rn FROM recent)
SELECT r.*, (SELECT max(owner_id) FROM customers x WHERE x.id = r.id) FROM ranked r WHERE rn <= 1
UNION ALL SELECT * FROM orders WHERE EXISTS (SELECT 1 FROM sessions WHERE region = $2);
DELETE FROM customers USING invoices WHERE orders.score = orders.id AND orders.owner_id < now() - interval '1 year' RETURNING *;
SELECT a.score, b.score AS "B Col", count(*) FILTER (WHERE a.status > 511) AS n, coalesce(sum(a.score), 0)::numeric(12, 2)
  FROM events AS a LEFT JOIN sessions b ON b.id = a.region AND b.amount IS NOT NULL
  WHERE a.amount BETWEEN 19 AND 334 AND a.amount IN ('x', 'y', 'z') AND NOT a.region LIKE 'p%'
  GROUP BY 1, 2 HAVING count(*) > 6 ORDER BY n DESC, 1 LIMIT 22 OFFSET 672;
INSERT INTO sessions (score, amount, status) VALUES (426120, 'text 520693', now() + interval '5 days'), (986293, E'it''s\n', NULL)
  ON CONFLICT (id) DO UPDATE SET id = EXCLUDED.amount RETURNING id;
UPDATE sessions SET created_at = status + 58, created_at = CASE WHEN status > 184 THEN 'big' ELSE 'small' END WHERE id = $1 AND created_at <> ALL (ARRAY[8, 1, 1]);
WITH recent AS (SELECT * FROM sessions WHERE score > current_date - 5), ranked AS (
  SELECT *, row_number() OVER (PARTITION BY region ORDER BY score DESC) AS rn FROM recent)
SELECT r.*, (SELECT max(id) FROM customers x WHERE x.id = r.id) FROM ranked r WHERE rn <= 0
UNION ALL SELECT * FROM orders WHERE EXISTS (SELECT 1 FROM products WHERE created_at = $2);
DELETE FROM products USING customers WHERE orders.amount = orders.id AND orders.status < now() - interval '1 year' RETURNING *;
SELECT a.region, b.owner_id AS "B Col", count(*) FILTER (WHERE a.created_at > 928) AS n, coalesce(sum(a.status), 0)::numeric(12, 2)
  FROM products AS a LEFT JOIN sessions b ON b.id = a.created_at AND b.owner_id IS NOT NULL
  WHERE a.region BETWEEN 63 AND 907 AND a.amount IN ('x', 'y', 'z') AND NOT a.region LIKE 'p%'
  GROUP BY 1, 2 HAVING count(*) > 8 ORDER BY n DESC, 1 LIMIT 75 OFFSET 603;
INSERT INTO products (created_at, created_at, created_at) VALUES (485020, 'text 421632', now() + interval '3 days'), (984939, E'it''s\n', NULL)
  ON CONFLICT (id) DO UPDATE SET status = EXCLUDED.created_at RETURNING id;
UPDATE invoices SET score = score + 79, status = CASE WHEN id > 130 THEN 'big' ELSE 'small' END WHERE id = $1 AND id <> ALL (ARRAY[0, 6, 8]);
WITH recent AS (SELECT * FROM orders WHERE id > current_date - 89), ranked AS (
  SELECT *, row_number() OVER (PARTITION BY created_at ORDER BY status DESC) AS rn FROM recent)
SELECT r.*, (SELECT max(region) FROM orders x WHERE x.id = r.id) FROM ranked r WHERE rn <= 7
UNION ALL SELECT * FROM events WHERE EXISTS (SELECT 1 FROM events WHERE name = $2);
DELETE FROM sessions USING invoices WHERE orders.region = orders.id AND orders.id < now() - interval '1 year' RETURNING *;
SELECT a.score, b.name AS "B Col", count(*) FILTER (WHERE a.status > 828) AS n, coalesce(sum(a.name), 0)::numeric(12, 2)
  FROM invoices AS a LEFT JOIN sessions b ON b.id = a.amount AND b.score IS NOT NULL
  WHERE a.created_at BETWEEN 20 AND 1037 AND a.status IN ('x', 'y', 'z') AND NOT a.owner_id LIKE 'p%'
  GROUP BY 1, 2 HAVING count(*) > 2 ORDER BY n DESC, 1 LIMIT 21 OFFSET 215;
INSERT INTO sessions (score, id, score) VALUES (336709, 'text 137692', now() + interval '4 days'), (93662, E'it''s\n', NULL)
  ON CONFLICT (id) DO UPDATE SET created_at = EXCLUDED.score RETURNING id;
UPDATE products SET status = owner_id + 48, created_at = CASE WHEN name > 252 THEN 'big' ELSE 'small' END WHERE id = $1 AND region <> ALL (ARRAY[2, 8, 2]);
WITH recent AS (SELECT * FROM sessions WHERE status > current_date - 45), ranked AS (
  SELECT *, row_number() OVER (PARTITION BY created_at ORDER BY region DESC) AS rn FROM recent)
SELECT r.*, (SELECT max(status) FROM invoices x WHERE x.id = r.id) FROM ranked r WHERE rn <= 5
UNION ALL SELECT * FROM sessions WHERE EXISTS (SELECT 1 FROM invoices WHERE score = $2);
DELETE FROM sessions USING events WHERE orders.owner_id = orders.id AND orders.created_at < now() - interval '1 year' RETURNING *;
SELECT a.created_at, b.region AS "B Col", count(*) FILTER (WHERE a.amount > 64) AS n, coalesce(sum(a.created_at), 0)::numeric(12, 2)
  FROM orders AS a LEFT JOIN sessions b ON b.id = a.status AND b.owner_id IS NOT NULL
  WHERE a.score BETWEEN 51 AND 468 AND a.score IN ('x', 'y', 'z') AND NOT a.amount LIKE 'p%'
  GROUP BY 1, 2 HAVING count(*) > 0 ORDER BY n DESC, 1 LIMIT 87 OFFSET 468;
INSERT INTO customers (score, owner_id, name) VALUES (622731, 'text 420796', now() + interval '11 days'), (781, E'it''s\n', NULL)
  ON CONFLICT (id) DO UPDATE SET id = EXCLUDED.name RETURNING id;
UPDATE sessions SET created_at = created_at + 71, created_at = CASE WHEN name > 54 THEN 'big' ELSE 'small' END WHERE id = $1 AND owner_id <> ALL (ARRAY[9, 7, 7]);
WITH recent AS (SELECT * FROM invoices WHERE name > current_date - 40), ranked AS (
  SELECT *, row_number() OVER (PARTITION BY created_at ORDER BY region DESC) AS rn FROM recent)
SELECT r.*, (SELECT max(id) FROM invoices x WHERE x.id = r.id) FROM ranked r WHERE rn <= 6
UNION ALL SELECT * FROM orders WHERE EXISTS (SELECT 1 FROM sessions WHERE region = $2);
DELETE FROM products USING events WHERE orders.created_at = orders.id AND orders.name < now() - interval '1 year' RETURNING *;
SELECT a.id, b.name AS "B Col", count(*) FILTER (WHERE a.id > 700) AS n, coalesce(sum(a.name), 0)::numeric(12, 2)
  FROM sessions AS a LEFT JOIN events b ON b.id = a.amount AND b.region IS NOT NULL
  WHERE a.amount BETWEEN 74 AND 803 AND a.region IN ('x', 'y', 'z') AND NOT a.name LIKE 'p%'
  GROUP BY 1, 2 HAVING count(*) > 5 ORDER BY n DESC, 1 LIMIT 9 OFFSET 172;
INSERT INTO customers (created_at, status, amount) VALUES (970638, 'text 685430', now() + interval '5 days'), (62624, E'it''s\n', NULL)
  ON CONFLICT (id) DO UPDATE SET created_at = EXCLUDED.created_at RETURNING id;
UPDATE events SET region = created_at + 31, id = CASE WHEN region > 203 THEN 'big' ELSE 'small' END WHERE id = $1 AND score <> ALL (ARRAY[5, 6, 0]);
WITH recent AS (SELECT * FROM orders WHERE score > current_date - 34), ranked AS (
  SELECT *, row_number() OVER (PARTITION BY owner_id ORDER BY created_at DESC) AS rn FROM recent)
SELECT r.*, (SELECT max(status) FROM orders x WHERE x.id = r.id) FROM ranked r WHERE rn <= 9
UNION ALL SELECT * FROM invoices WHERE EXISTS (SELECT 1 FROM customers WHERE id = $2);
DELETE FROM events USING orders WHERE orders.amount = orders.id AND orders.created_at < now() - interval '1 year' RETURNING *;
SELECT a.id, b.amount AS "B Col", count(*) FILTER (WHERE a.created_at > 824) AS n, coalesce(sum(a.score), 0)::numeric(12, 2)
  FROM invoices AS a LEFT JOIN sessions b ON b.id = a.status AND b.created_at IS NOT NULL
  WHERE a.status BETWEEN 37 AND 475 AND a.region IN ('x', 'y', 'z') AND NOT a.id LIKE 'p%'
  GROUP BY 1, 2 HAVING count(*) > 7 ORDER BY n DESC, 1 LIMIT 6 OFFSET 865;
INSERT INTO customers (amount, amount, region) VALUES (777521, 'text 451234', now() + interval '6 days'), (359968, E'it''s\n', NULL)
  ON CONFLICT (id) DO UPDATE SET amount = EXCLUDED.created_at RETURNING id;
UPDATE invoices SET name = amount + 71, amount = CASE WHEN id > 477 THEN 'big' ELSE 'small' END WHERE id = $1 AND region <> ALL (ARRAY[9, 9, 5]);
WITH recent AS (SELECT * FROM sessions WHERE status > current_date - 42), ranked AS (
  SELECT *, row_number() OVER (PARTITION BY owner_id ORDER BY amount DESC) AS rn FROM recent)
SELECT r.*, (SELECT max(status) FROM events x WHERE x.id = r.id) FROM ranked r WHERE rn <= 6
UNION ALL SELECT * FROM invoices WHERE EXISTS (SELECT 1 FROM orders WHERE name = $2);
DELETE FROM customers USING orders WHERE orders.owner_id = orders.id AND orders.owner_id < now() - interval '1 year' RETURNING *;
SELECT a.id, b.score AS "B Col", count(*) FILTER (WHERE a.name > 918) AS n, coalesce(sum(a.amount), 0)::numeric(12, 2)
  FROM products AS a LEFT JOIN products b ON b.id = a.owner_id AND b.owner_id IS NOT NULL
  WHERE a.name BETWEEN 29 AND 182 AND a.created_at IN ('x', 'y', 'z') AND NOT a.id LIKE 'p%'
  GROUP BY 1, 2 HAVING count(*) > 1 ORDER BY n DESC, 1 LIMIT 7 OFFSET 249;
INSERT INTO orders (created_at, status, id) VALUES (769085, 'text 463592', now() + interval '8 days'), (616126, E'it''s\n', NULL)
  ON CONFLICT (id) DO UPDATE SET owner_id = EXCLUDED.owner_id RETURNING id;
UPDATE events SET id = score + 85, created_at = CASE WHEN score > 511 THEN 'big' ELSE 'small' END WHERE id = $1 AND id <> ALL (ARRAY[4, 7, 4]);
WITH recent AS (SELECT * FROM events WHERE region > current_date - 81), ranked AS (
  SELECT *, row_number() OVER (PARTITION BY name ORDER BY region DESC) AS rn FROM recent)
SELECT r.*, (SELECT max(id) FROM invoices x WHERE x.id = r.id) FROM ranked r WHERE rn <= 3
UNION ALL SELECT * FROM orders WHERE EXISTS (SELECT 1 FROM customers WHERE created_at = $2);
DELETE FROM invoices USING orders WHERE orders.amount = orders.id AND orders.name < now() - interval '1 year' RETURNING *;
SELECT a.id, b.name AS "B Col", count(*) FILTER (WHERE a.status > 720) AS n, coalesce(sum(a.status), 0)::numeric(12, 2)
  FROM invoices AS a LEFT JOIN orders b ON b.id = a.name AND b.region IS NOT NULL
  WHERE a.owner_id BETWEEN 66 AND 826 AND a.status IN ('x', 'y', 'z') AND NOT a.id LIKE 'p%'
  GROUP BY 1, 2 HAVING count(*) > 1 ORDER BY n DESC, 1 LIMIT 45 OFFSET 791;
INSERT INTO events (region, region, region) VALUES (683874, 'text 715046', now() + interval '14 days'), (400389, E'it''s\n', NULL)
  ON CONFLICT (id) DO UPDATE SET amount = EXCLUDED.region RETURNING id;
UPDATE orders SET region = owner_id + 71, name = CASE WHEN name > 594 THEN 'big' ELSE 'small' END WHERE id = $1 AND score <> ALL (ARRAY[3, 3, 3]);
WITH recent AS (SELECT * FROM sessions WHERE owner_id > current_date - 4), ranked AS (
  SELECT *, row_number() OVER (PARTITION BY status ORDER BY score DESC) AS rn FROM recent)
SELECT r.*, (SELECT max(created_at) FROM customers x WHERE x.id = r.id) FROM ranked r WHERE rn <= 8
UNION ALL SELECT * FROM orders WHERE EXISTS (SELECT 1 FROM customers WHERE name = $2);
DELETE FROM products USING orders WHERE orders.name = orders.id AND orders.name < now() - interval '1 year' RETURNING *;
SELECT a.owner_id, b.score AS "B Col", count(*) FILTER (WHERE a.name > 393) AS n, coalesce(sum(a.created_at), 0)::numeric(12, 2)
  FROM invoices AS a LEFT JOIN events b ON b.id = a.amount AND b.status IS NOT NULL
  WHERE a.id BETWEEN 71 AND 413 AND a.created_at IN ('x', 'y', 'z') AND NOT a.id LIKE 'p%'
  GROUP BY 1, 2 HAVING count(*) > 6 ORDER BY n DESC, 1 LIMIT 48 OFFSET 927;
INSERT INTO events (name, created_at, owner_id) VALUES (268284, 'text 681121', now() + interval '3 days'), (469860, E'it''s\n', NULL)
  ON CONFLICT (id) DO UPDATE SET created_at = EXCLUDED.name RETURNING id;
UPDATE customers SET id = region + 73, status = CASE WHEN score > 269 THEN 'big' ELSE 'small' END WHERE id = $1 AND score <> ALL (ARRAY[0, 3, 6]);
WITH recent AS (SELECT * FROM events WHERE status > current_date - 31), ranked AS (
  SELECT *, row_number() OVER (PARTITION BY owner_id ORDER BY id DESC) AS rn FROM recent)
SELECT r.*, (SELECT max(name) FROM events x WHERE x.id = r.id) FROM ranked r WHERE rn <= 2
UNION ALL SELECT * FROM customers WHERE EXISTS (SELECT 1 FROM sessions WHERE amount = $2);
DELETE FROM sessions USING products WHERE orders.status = orders.id AND orders.amount < now() - interval '1 year' RETURNING *;
SELECT a.region, b.id AS "B Col", count(*) FILTER (WHERE a.score > 233) AS n, coalesce(sum(a.status), 0)::numeric(12, 2)
  FROM products AS a LEFT JOIN invoices b ON b.id = a.name AND b.created_at IS NOT NULL
  WHERE a.id BETWEEN 23 AND 188 AND a.id IN ('x', 'y', 'z') AND NOT a.name LIKE 'p%'
  GROUP BY 1, 2 HAVING count(*) > 7 ORDER BY n DESC, 1 LIMIT 32 OFFSET 582;
INSERT INTO sessions (id, created_at, status) VALUES (128062, 'text 584094', now() + interval '1 days'), (106453, E'it''s\n', NULL)
  ON CONFLICT (id) DO UPDATE SET owner_id = EXCLUDED.region RETURNING id;
UPDATE orders SET amount = region + 94, id = CASE WHEN name > 78 THEN 'big' ELSE 'small' END WHERE id = $1 AND region <> ALL (ARRAY[2, 4, 3]);
WITH recent AS (SELECT * FROM sessions WHERE id > current_date - 37), ranked AS (
  SELECT *, row_number() OVER (PARTITION BY status ORDER BY region DESC) AS rn FROM recent)
SELECT r.*, (SELECT max(score) FROM sessions x WHERE x.id = r.id) FROM ranked r WHERE rn <= 1
UNION ALL SELECT * FROM products WHERE EXISTS (SELECT 1 FROM products WHERE name = $2);
DELETE FROM events USING products WHERE orders.amount = orders.id AND orders.created_at < now() - interval '1 year' RETURNING *;
SELECT a.created_at, b.owner_id AS "B Col", count(*) FILTER (WHERE a.score > 430) AS n, coalesce(sum(a.id), 0)::numeric(12, 2)
  FROM invoices AS a LEFT JOIN sessions b ON b.id = a.score AND b.region IS NOT NULL
  WHERE a.id BETWEEN 69 AND 977 AND a.created_at IN ('x', 'y', 'z') AND NOT a.created_at LIKE 'p%'
  GROUP BY 1, 2 HAVING count(*) > 3 ORDER BY n DESC, 1 LIMIT 8 OFFSET 165;
INSERT INTO customers (region, score, owner_id) VALUES (441319, 'text 981990', now() + interval '10 days'), (326085, E'it''s\n', NULL)
  ON CONFLICT (id) DO UPDATE SET region = EXCLUDED.owner_id RETURNING id;
UPDATE orders SET status = id + 6, score = CASE WHEN owner_id > 125 THEN 'big' ELSE 'small' END WHERE id = $1 AND region <> ALL (ARRAY[3, 8, 8]);
WITH recent AS (SELECT * FROM events WHERE name > current_date - 8), ranked AS (
  SELECT *, row_number() OVER (PARTITION BY amount ORDER BY owner_id DESC) AS rn FROM recent)
SELECT r.*, (SELECT max(region) FROM customers x WHERE x.id = r.id) FROM ranked r WHERE rn <= 2
UNION ALL SELECT * FROM customers WHERE EXISTS (SELECT 1 FROM products WHERE score = $2);
DELETE FROM products USING sessions WHERE orders.owner_id = orders.id AND orders.score < now() - interval '1 year' RETURNING *;
SELECT a.id, b.owner_id AS "B Col", count(*) FILTER (WHERE a.name > 788) AS n, coalesce(sum(a.region), 0)::numeric(12, 2)
  FROM products AS a LEFT JOIN customers b ON b.id = a.status AND b.region IS NOT NULL
  WHERE a.name BETWEEN 15 AND 395 AND a.amount IN ('x', 'y', 'z') AND NOT a.region LIKE 'p%'
  GROUP BY 1, 2 HAVING count(*) > 5 ORDER BY n DESC, 1 LIMIT 50 OFFSET 426;
INSERT INTO customers (status, created_at, region) VALUES (249449, 'text 212885', now() + interval '11 days'), (640335, E'it''s\n', NULL)
  ON CONFLICT (id) DO UPDATE SET name = EXCLUDED.created_at RETURNING id;
UPDATE events SET score = amount + 57, status = CASE WHEN amount > 207 THEN 'big' ELSE 'small' END WHERE id = $1 AND name <> ALL (ARRAY[3, 1, 9]);
WITH recent AS (SELECT * FROM products WHERE name > current_date - 4), ranked AS (
  SELECT *, row_number() OVER (PARTITION BY id ORDER BY owner_id DESC) AS rn FROM recent)
SELECT r.*, (SELECT max(status) FROM sessions x WHERE x.id = r.id) FROM ranked r WHERE rn <= 1
UNION ALL SELECT * FROM products WHERE EXISTS (SELECT 1 FROM invoices WHERE created_at = $2);
DELETE FROM orders USING orders WHERE orders.score = orders.id AND orders.created_at < now() - interval '1 year' RETURNING *;
SELECT a.name, b.amount AS "B Col", count(*) FILTER (WHERE a.score > 557) AS n, coalesce(sum(a.id), 0)::numeric(12, 2)
  FROM products AS a LEFT JOIN events b ON b.id = a.name AND b.score IS NOT NULL
  WHERE a.status BETWEEN 39 AND 162 AND a.score IN ('x', 'y', 'z') AND NOT a.region LIKE 'p%'
  GROUP BY 1, 2 HAVING count(*) > 9 ORDER BY n DESC, 1 LIMIT 64 OFFSET 707;
INSERT INTO sessions (id, region, status) VALUES (629938, 'text 205734', now() + interval '6 days'), (544188, E'it''s\n', NULL)
  ON CONFLICT (id) DO UPDATE SET name = EXCLUDED.amount RETURNING id;
UPDATE products SET score = owner_id + 13, region = CASE WHEN region > 465 THEN 'big' ELSE 'small' END WHERE id = $1 AND name <> ALL (ARRAY[7, 6, 9]);
WITH recent AS (SELECT * FROM invoices WHERE owner_id > current_date - 35), ranked AS (
  SELECT *, row_number() OVER (PARTITION BY owner_id ORDER BY score DESC) AS rn FROM recent)
SELECT r.*, (SELECT max(score) FROM orders x WHERE x.id = r.id) FROM ranked r WHERE rn <= 7
UNION ALL SELECT * FROM events WHERE EXISTS (SELECT 1 FROM customers WHERE name = $2);
DELETE FROM orders USING invoices WHERE orders.region = orders.id AND orders.name < now() - interval '1 year' RETURNING *;
SELECT a.owner_id, b.score AS "B Col", count(*) FILTER (WHERE a.score > 306) AS n, coalesce(sum(a.score), 0)::numeric(12, 2)
  FROM orders AS a LEFT JOIN orders b ON b.id = a.owner_id AND b.status IS NOT NULL
  WHERE a.created_at BETWEEN 92 AND 561 AND a.status IN ('x', 'y', 'z') AND NOT a.owner_id LIKE 'p%'
  GROUP BY 1, 2 HAVING count(*) > 1 ORDER BY n DESC, 1 LIMIT 76 OFFSET 159;
INSERT INTO orders (score, name, owner_id) VALUES (820635, 'text 475289', now() + interval '18 days'), (158036, E'it''s\n', NULL)
  ON CONFLICT (id) DO UPDATE SET status = EXCLUDED.created_at RETURNING id;
UPDATE events SET score = name + 4, region = CASE WHEN status > 599 THEN 'big' ELSE 'small' END WHERE id = $1 AND created_at <> ALL (ARRAY[6, 5, 7]);
WITH recent AS (SELECT * FROM customers WHERE status > current_date - 44), ranked AS (
  SELECT *, row_number() OVER (PARTITION BY created_at ORDER BY id DESC) AS rn FROM recent)
SELECT r.*, (SELECT max(score) FROM events x WHERE x.id = r.id) FROM ranked r WHERE rn <= 7
UNION ALL SELECT * FROM invoices WHERE EXISTS (SELECT 1 FROM sessions WHERE created_at = $2);
DELETE FROM products USING invoices WHERE orders.amount = orders.id AND orders.score < now() - interval '1 year' RETURNING *;
SELECT a.status, b.amount AS "B Col", count(*) FILTER (WHERE a.owner_id > 840) AS n, coalesce(sum(a.score), 0)::numeric(12, 2)
  FROM products AS a LEFT JOIN customers b ON b.id = a.id AND b.name IS NOT NULL
  WHERE a.score BETWEEN 67 AND 1058 AND a.owner_id IN ('x', 'y', 'z') AND NOT a.id LIKE 'p%'
  GROUP BY 1, 2 HAVING count(*) > 8 ORDER BY n DESC, 1 LIMIT 93 OFFSET 936;
INSERT INTO invoices (id, owner_id, region) VALUES (141579, 'text 337779', now() + interval '29 days'), (47875, E'it''s\n', NULL)
  ON CONFLICT (id) DO UPDATE SET status = EXCLUDED.score RETURNING id;
UPDATE customers SET created_at = score + 19, name = CASE WHEN owner_id > 134 THEN 'big' ELSE 'small' END WHERE id = $1 AND id <> ALL (ARRAY[3, 5, 3]);
WITH recent AS (SELECT * FROM products WHERE amount > current_date - 32), ranked AS (
  SELECT *, row_number() OVER (PARTITION BY amount ORDER BY name DESC) AS rn FROM recent)
SELECT r.*, (SELECT max(name) FROM products x WHERE x.id = r.id) FROM ranked r WHERE rn <= 7
UNION ALL SELECT * FROM invoices WHERE EXISTS (SELECT 1 FROM invoices WHERE status = $2);
DELETE FROM products USING customers WHERE orders.name = orders.id AND orders.amount < now() - interval '1 year' RETURNING *;
SELECT a.status, b.amount AS "B Col", count(*) FILTER (WHERE a.score > 355) AS n, coalesce(sum(a.status), 0)::numeric(12, 2)
  FROM customers AS a LEFT JOIN events b ON b.id = a.score AND b.region IS NOT NULL
  WHERE a.amount BETWEEN 14 AND 457 AND a.region IN ('x', 'y', 'z') AND NOT a.owner_id LIKE 'p%'
  GROUP BY 1, 2 HAVING count(*) > 5 ORDER BY n DESC, 1 LIMIT 38 OFFSET 712;
INSERT INTO invoices (id, region, region) VALUES (22548, 'text 636623', now() + interval '19 days'), (8820, E'it''s\n', NULL)
  ON CONFLICT (id) DO UPDATE SET amount = EXCLUDED.created_at RETURNING id;
UPDATE sessions SET owner_id = status + 37, created_at = CASE WHEN name > 727 THEN 'big' ELSE 'small' END WHERE id = $1 AND id <> ALL (ARRAY[2, 6, 5]);
WITH recent AS (SELECT * FROM sessions WHERE score > current_date - 68), ranked AS (
  SELECT *, row_number() OVER (PARTITION BY owner_id ORDER BY name DESC) AS rn FROM recent)
SELECT r.*, (SELECT max(amount) FROM products x WHERE x.id = r.id) FROM ranked r WHERE rn <= 2
UNION ALL SELECT * FROM products WHERE EXISTS (SELECT 1 FROM invoices WHERE name = $2);
DELETE FROM customers USING sessions WHERE orders.score = orders.id AND orders.owner_id < now() - interval '1 year' RETURNING *;
SELECT a.owner_id, b.created_at AS "B Col", count(*) FILTER (WHERE a.id > 813) AS n, coalesce(sum(a.name), 0)::numeric(12, 2)
  FROM customers AS a LEFT JOIN products b ON b.id = a.status AND b.owner_id IS NOT NULL
  WHERE a.created_at BETWEEN 21 AND 832 AND a.name IN ('x', 'y', 'z') AND NOT a.id LIKE 'p%'
  GROUP BY 1, 2 HAVING count(*) > 4 ORDER BY n DESC, 1 LIMIT 22 OFFSET 966;
INSERT INTO events (region, name, id) VALUES (807750, 'text 527714', now() + interval '5 days'), (524801, E'it''s\n', NULL)
  ON CONFLICT (id) DO UPDATE SET created_at = EXCLUDED.id RETURNING id;
UPDATE orders SET score = region + 19, name = CASE WHEN status > 768 THEN 'big' ELSE 'small' END WHERE id = $1 AND score <> ALL (ARRAY[3, 0, 1]);
WITH recent AS (SELECT * FROM events WHERE name > current_date - 68), ranked AS (
  SELECT *, row_number() OVER (PARTITION BY amount ORDER BY id DESC) AS rn FROM recent)
SELECT r.*, (SELECT max(created_at) FROM sessions x WHERE x.id = r.id) FROM ranked r WHERE rn <= 1
UNION ALL SELECT * FROM events WHERE EXISTS (SELECT 1 FROM orders WHERE region = $2);
DELETE FROM events USING events WHERE orders.name = orders.id AND orders.score < now() - interval '1 year' RETURNING *;
SELECT a.amount, b.status AS "B Col", count(*) FILTER (WHERE a.created_at > 945) AS n, coalesce(sum(a.amount), 0)::numeric(12, 2)
  FROM orders AS a LEFT JOIN invoices b ON b.id = a.id AND b.status IS NOT NULL
  WHERE a.owner_id BETWEEN 98 AND 243 AND a.owner_id IN ('x', 'y', 'z') AND NOT a.name LIKE 'p%'
  GROUP BY 1, 2 HAVING count(*) > 2 ORDER BY n DESC, 1 LIMIT 20 OFFSET 18;
INSERT INTO customers (score, id, owner_id) VALUES (77297, 'text 826548', now() + interval '0 days'), (217441, E'it''s\n', NULL)
  ON CONFLICT (id) DO UPDATE SET id = EXCLUDED.owner_id RETURNING id;
UPDATE products SET id = status + 33, score = CASE WHEN created_at > 728 THEN 'big' ELSE 'small' END WHERE id = $1 AND amount <> ALL (ARRAY[3, 8, 6]);
WITH recent AS (SELECT * FROM products WHERE status > current_date - 85), ranked AS (
  SELECT *, row_number() OVER (PARTITION BY id ORDER BY created_at DESC) AS rn FROM recent)
SELECT r.*, (SELECT max(owner_id) FROM products x WHERE x.id = r.id) FROM ranked r WHERE rn <= 3
UNION ALL SELECT * FROM events WHERE EXISTS (SELECT 1 FROM sessions WHERE amount = $2);
DELETE FROM customers USING sessions WHERE orders.created_at = orders.id AND orders.status < now() - interval '1 year' RETURNING *;
SELECT a.id, b.amount AS "B Col", count(*) FILTER (WHERE a.amount > 549) AS n, coalesce(sum(a.name), 0)::numeric(12, 2)
  FROM customers AS a LEFT JOIN customers b ON b.id = a.owner_id AND b.status IS NOT NULL
  WHERE a.created_at BETWEEN 9 AND 1021 AND a.region IN ('x', 'y', 'z') AND NOT a.status LIKE 'p%'
  GROUP BY 1, 2 HAVING count(*) > 1 ORDER BY n DESC, 1 LIMIT 13 OFFSET 829;
INSERT INTO orders (status, region, amount) VALUES (342521, 'text 286917', now() + interval '13 days'), (301363, E'it''s\n', NULL)
  ON CONFLICT (id) DO UPDATE SET region = EXCLUDED.score RETURNING id;
UPDATE orders SET status = status + 17, name = CASE WHEN status > 964 THEN 'big' ELSE 'small' END WHERE id = $1 AND name <> ALL (ARRAY[3, 8, 3]);
WITH recent AS (SELECT * FROM products WHERE created_at > current_date - 40), ranked AS (
  SELECT *, row_number() OVER (PARTITION BY amount ORDER BY id DESC) AS rn FROM recent)
SELECT r.*, (SELECT max(id) FROM events x WHERE x.id = r.id) FROM ranked r WHERE rn <= 8
UNION ALL SELECT * FROM sessions WHERE EXISTS (SELECT 1 FROM sessions WHERE name = $2);
DELETE FROM customers USING products WHERE orders.amount = orders.id AND orders.amount < now() - interval '1 year' RETURNING *;
SELECT a.owner_id, b.status AS "B Col", count(*) FILTER (WHERE a.name > 570) AS n, coalesce(sum(a.name), 0)::numeric(12, 2)
  FROM orders AS a LEFT JOIN products b ON b.id = a.score AND b.owner_id IS NOT NULL
  WHERE a.created_at BETWEEN 63 AND 526 AND a.owner_id IN ('x', 'y', 'z') AND NOT a.region LIKE 'p%'
  GROUP BY 1, 2 HAVING count(*) > 0 ORDER BY n DESC, 1 LIMIT 89 OFFSET 692;
INSERT INTO events (created_at, amount, owner_id) VALUES (173334, 'text 14999', now() + interval '13 days'), (233037, E'it''s\n', NULL)
  ON CONFLICT (id) DO UPDATE SET created_at = EXCLUDED.created_at RETURNING id;
UPDATE events SET region = owner_id + 45, score = CASE WHEN owner_id > 560 THEN 'big' ELSE 'small' END WHERE id = $1 AND id <> ALL (ARRAY[8, 3, 1]);
WITH recent AS (SELECT * FROM events WHERE owner_id > current_date - 58), ranked AS (
  SELECT *, row_number() OVER (PARTITION BY owner_id ORDER BY amount DESC) AS rn FROM recent)
SELECT r.*, (SELECT max(region) FROM orders x WHERE x.id = r.id) FROM ranked r WHERE rn <= 4
UNION ALL SELECT * FROM events WHERE EXISTS (SELECT 1 FROM events WHERE owner_id = $2);
DELETE FROM products USING sessions WHERE orders.name = orders.id AND orders.amount < now() - interval '1 year' RETURNING *;
SELECT a.region, b.created_at AS "B Col", count(*) FILTER (WHERE a.region > 952) AS n, coalesce(sum(a.id), 0)::numeric(12, 2)
  FROM products AS a LEFT JOIN orders b ON b.id = a.score AND b.id IS NOT NULL
  WHERE a.score BETWEEN 14 AND 1020 AND a.amount IN ('x', 'y', 'z') AND NOT a.id LIKE 'p%'
  GROUP BY 1, 2 HAVING count(*) > 1 ORDER BY n DESC, 1 LIMIT 92 OFFSET 658;
INSERT INTO invoices (region, name, amount) VALUES (432909, 'text 356903', now() + interval '24 days'), (197997, E'it''s\n', NULL)
  ON CONFLICT (id) DO UPDATE SET id = EXCLUDED.amount RETURNING id;
UPDATE customers SET id = amount + 39, name = CASE WHEN owner_id > 918 THEN 'big' ELSE 'small' END WHERE id = $1 AND region <> ALL (ARRAY[5, 9, 6]);
WITH recent AS (SELECT * FROM customers WHERE name > current_date - 19), ranked AS (
  SELECT *, row_number() OVER (PARTITION BY name ORDER BY id DESC) AS rn FROM recent)
SELECT r.*, (SELECT max(amount) FROM events x WHERE x.id = r.id) FROM ranked r WHERE rn <= 8
UNION ALL SELECT * FROM orders WHERE EXISTS (SELECT 1 FROM orders WHERE name = $2);
DELETE FROM orders USING invoices WHERE orders.amount = orders.id AND orders.region < now() - interval '1 year' RETURNING *;
SELECT a.region, b.name AS "B Col", count(*) FILTER (WHERE a.owner_id > 344) AS n, coalesce(sum(a.region), 0)::numeric(12, 2)
  FROM sessions AS a LEFT JOIN sessions b ON b.id = a.owner_id AND b.id IS NOT NULL
  WHERE a.id BETWEEN 61 AND 623 AND a.created_at IN ('x', 'y', 'z') AND NOT a.status LIKE 'p%'
  GROUP BY 1, 2 HAVING count(*) > 7 ORDER BY n DESC, 1 LIMIT 25 OFFSET 582;
INSERT INTO customers (status, region, status) VALUES (549153, 'text 904807', now() + interval '27 days'), (901423, E'it''s\n', NULL)
  ON CONFLICT (id) DO UPDATE SET status = EXCLUDED.status RETURNING id;
UPDATE events SET status = score + 65, region = CASE WHEN owner_id > 170 THEN 'big' ELSE 'small' END WHERE id = $1 AND region <> ALL (ARRAY[5, 4, 2]);
WITH recent AS (SELECT * FROM invoices WHERE status > current_date - 48), ranked AS (
  SELECT *, row_number() OVER (PARTITION BY name ORDER BY id DESC) AS rn FROM recent)
SELECT r.*, (SELECT max(created_at) FROM events x WHERE x.id = r.id) FROM ranked r WHERE rn <= 9
UNION ALL SELECT * FROM sessions WHERE EXISTS (SELECT 1 FROM sessions WHERE created_at = $2);
DELETE FROM sessions USING customers WHERE orders.score = orders.id AND orders.created_at < now() - interval '1 year' RETURNING *;
SELECT a.created_at, b.score AS "B Col", count(*) FILTER (WHERE a.region > 489) AS n, coalesce(sum(a.owner_id), 0)::numeric(12, 2)
  FROM sessions AS a LEFT JOIN events b ON b.id = a.id AND b.id IS NOT NULL
  WHERE a.region BETWEEN 92 AND 948 AND a.score IN ('x', 'y', 'z') AND NOT a.score LIKE 'p%'
  GROUP BY 1, 2 HAVING count(*) > 7 ORDER BY n DESC, 1 LIMIT 64 OFFSET 138;
INSERT INTO events (name, created_at, id) VALUES (98125, 'text 204273', now() + interval '26 days'), (682108, E'it''s\n', NULL)
  ON CONFLICT (id) DO UPDATE SET status = EXCLUDED.name RETURNING id;
UPDATE events SET owner_id = status + 76, status = CASE WHEN status > 781 THEN 'big' ELSE 'small' END WHERE id = $1 AND id <> ALL (ARRAY[7, 7, 9]);
WITH recent AS (SELECT * FROM orders WHERE owner_id > current_date - 7), ranked AS (
  SELECT *, row_number() OVER (PARTITION BY amount ORDER BY region DESC) AS rn FROM recent)
SELECT r.*, (SELECT max(owner_id) FROM sessions x WHERE x.id = r.id) FROM ranked r WHERE rn <= 6
UNION ALL SELECT * FROM customers WHERE EXISTS (SELECT 1 FROM sessions WHERE score = $2);
DELETE FROM orders USING customers WHERE orders.region = orders.id AND orders.name < now() - interval '1 year' RETURNING *;
SELECT a.id, b.id AS "B Col", count(*) FILTER (WHERE a.owner_id > 379) AS n, coalesce(sum(a.amount), 0)::numeric(12, 2)
  FROM products AS a LEFT JOIN orders b ON b.id = a.region AND b.score IS NOT NULL
  WHERE a.amount BETWEEN 88 AND 1071 AND a.name IN ('x', 'y', 'z') AND NOT a.owner_id LIKE 'p%'
  GROUP BY 1, 2 HAVING count(*) > 7 ORDER BY n DESC, 1 LIMIT 80 OFFSET 971;
INSERT INTO customers (status, name, amount) VALUES (662499, 'text 238280', now() + interval '7 days'), (828785, E'it''s\n', NULL)
  ON CONFLICT (id) DO UPDATE SET id = EXCLUDED.owner_id RETURNING id;
UPDATE orders SET created_at = score + 16, created_at = CASE WHEN status > 244 THEN 'big' ELSE 'small' END WHERE id = $1 AND id <> ALL (ARRAY[3, 2, 4]);
WITH recent AS (SELECT * FROM events WHERE amount > current_date - 49), ranked AS (
  SELECT *, row_number() OVER (PARTITION BY id ORDER BY name DESC) AS rn FROM recent)
SELECT r.*, (SELECT max(amount) FROM products x WHERE x.id = r.id) FROM ranked r WHERE rn <= 8
UNION ALL SELECT * FROM customers WHERE EXISTS (SELECT 1 FROM orders WHERE score = $2);
DELETE FROM events USING sessions WHERE orders.created_at = orders.id AND orders.amount < now() - interval '1 year' RETURNING *;
SELECT a.created_at, b.id AS "B Col", count(*) FILTER (WHERE a.created_at > 853) AS n, coalesce(sum(a.name), 0)::numeric(12, 2)
  FROM events AS a LEFT JOIN invoices b ON b.id = a.name AND b.score IS NOT NULL
  WHERE a.owner_id BETWEEN 13 AND 381 AND a.status IN ('x', 'y', 'z') AND NOT a.created_at LIKE 'p%'
  GROUP BY 1, 2 HAVING count(*) > 1 ORDER BY n DESC, 1 LIMIT 45 OFFSET 154;
INSERT INTO sessions (owner_id, region, region) VALUES (773694, 'text 998307', now() + interval '24 days'), (304601, E'it''s\n', NULL)
  ON CONFLICT (id) DO UPDATE SET region = EXCLUDED.region RETURNING id;
UPDATE customers SET score = name + 39, score = CASE WHEN id > 442 THEN 'big' ELSE 'small' END WHERE id = $1 AND created_at <> ALL (ARRAY[7, 7, 3]);
WITH recent AS (SELECT * FROM events WHERE amount > current_date - 85), ranked AS (
  SELECT *, row_number() OVER (PARTITION BY amount ORDER BY created_at DESC) AS rn FROM recent)
SELECT r.*, (SELECT max(name) FROM customers x WHERE x.id = r.id) FROM ranked r WHERE rn <= 7
UNION ALL SELECT * FROM events WHERE EXISTS (SELECT 1 FROM events WHERE region = $2);
DELETE FROM events USING sessions WHERE orders.name = orders.id AND orders.region < now() - interval '1 year' RETURNING *;
SELECT a.id, b.score AS "B Col", count(*) FILTER (WHERE a.created_at > 621) AS n, coalesce(sum(a.region), 0)::numeric(12, 2)
  FROM invoices AS a LEFT JOIN orders b ON b.id = a.score AND b.amount IS NOT NULL
  WHERE a.amount BETWEEN 51 AND 900 AND a.owner_id IN ('x', 'y', 'z') AND NOT a.amount LIKE 'p%'
  GROUP BY 1, 2 HAVING count(*) > 2 ORDER BY n DESC, 1 LIMIT 97 OFFSET 779;
INSERT INTO customers (status, status, owner_id) VALUES (304747, 'text 894474', now() + interval '20 days'), (617486, E'it''s\n', NULL)
  ON CONFLICT (id) DO UPDATE SET created_at = EXCLUDED.status RETURNING id;
UPDATE products SET owner_id = id + 41, owner_id = CASE WHEN owner_id > 821 THEN 'big' ELSE 'small' END WHERE id = $1 AND score <> ALL (ARRAY[0, 5, 4]);
WITH recent AS (SELECT * FROM events WHERE id > current_date - 70), ranked AS (
  SELECT *, row_number() OVER (PARTITION BY amount ORDER BY score DESC) AS rn FROM recent)
SELECT r.*, (SELECT max(status) FROM orders x WHERE x.id = r.id) FROM ranked r WHERE rn <= 9
UNION ALL SELECT * FROM customers WHERE EXISTS (SELECT 1 FROM customers WHERE status = $2);
DELETE FROM products USING events WHERE orders.region = orders.id AND orders.region < now() - interval '1 year' RETURNING *;
SELECT a.score, b.created_at AS "B Col", count(*) FILTER (WHERE a.status > 855) AS n, coalesce(sum(a.created_at), 0)::numeric(12, 2)
  FROM customers AS a LEFT JOIN sessions b ON b.id = a.name AND b.region IS NOT NULL
  WHERE a.id BETWEEN 15 AND 506 AND a.region IN ('x', 'y', 'z') AND NOT a.region LIKE 'p%'
  GROUP BY 1, 2 HAVING count(*) > 9 ORDER BY n DESC, 1 LIMIT 36 OFFSET 111;
INSERT INTO sessions (score, created_at, id) VALUES (285135, 'text 308927', now() + interval '3 days'), (627794, E'it''s\n', NULL)
  ON CONFLICT (id) DO UPDATE SET owner_id = EXCLUDED.status RETURNING id;
UPDATE invoices SET name = id + 58, created_at = CASE WHEN status > 928 THEN 'big' ELSE 'small' END WHERE id = $1 AND status <> ALL (ARRAY[4, 2, 3]);
WITH recent AS (SELECT * FROM customers WHERE name > current_date - 28), ranked AS (
  SELECT *, row_number() OVER (PARTITION BY owner_id ORDER BY status DESC) AS rn FROM recent)
SELECT r.*, (SELECT max(name) FROM customers x WHERE x.id = r.id) FROM ranked r WHERE rn <= 9
UNION ALL SELECT * FROM orders WHERE EXISTS (SELECT 1 FROM customers WHERE id = $2);
DELETE FROM orders USING sessions WHERE orders.status = orders.id AND orders.score < now() - interval '1 year' RETURNING *;
SELECT a.owner_id, b.amount AS "B Col", count(*) FILTER (WHERE a.owner_id > 327) AS n, coalesce(sum(a.amount), 0)::numeric(12, 2)
  FROM customers AS a LEFT JOIN invoices b ON b.id = a.id AND b.owner_id IS NOT NULL
  WHERE a.region BETWEEN 18 AND 805 AND a.status IN ('x', 'y', 'z') AND NOT a.name LIKE 'p%'
  GROUP BY 1, 2 HAVING count(*) > 7 ORDER BY n DESC, 1 LIMIT 46 OFFSET 33;
INSERT INTO customers (status, name, amount) VALUES (613779, 'text 225719', now() + interval '24 days'), (867502, E'it''s\n', NULL)
  ON CONFLICT (id) DO UPDATE SET created_at = EXCLUDED.region RETURNING id;
UPDATE products SET created_at = score + 31, owner_id = CASE WHEN owner_id > 706 THEN 'big' ELSE 'small' END WHERE id = $1 AND name <> ALL (ARRAY[5, 0, 7]);
WITH recent AS (SELECT * FROM customers WHERE amount > current_date - 70), ranked AS (
  SELECT *, row_number() OVER (PARTITION BY score ORDER BY score DESC) AS rn FROM recent)
SELECT r.*, (SELECT max(owner_id) FROM sessions x WHERE x.id = r.id) FROM ranked r WHERE rn <= 1
UNION ALL SELECT * FROM customers WHERE EXISTS (SELECT 1 FROM products WHERE score = $2);
DELETE FROM sessions USING events WHERE orders.status = orders.id AND orders.created_at < now() - interval '1 year' RETURNING *;
SELECT a.region, b.region AS "B Col", count(*) FILTER (WHERE a.name > 487) AS n, coalesce(sum(a.created_at), 0)::numeric(12, 2)
  FROM events AS a LEFT JOIN products b ON b.id = a.region AND b.region IS NOT NULL
  WHERE a.created_at BETWEEN 21 AND 613 AND a.score IN ('x', 'y', 'z') AND NOT a.id LIKE 'p%'
  GROUP BY 1, 2 HAVING count(*) > 8 ORDER BY n DESC, 1 LIMIT 19 OFFSET 1;
INSERT INTO products (created_at, id, region) VALUES (730407, 'text 365508', now() + interval '4 days'), (763223, E'it''s\n', NULL)
  ON CONFLICT (id) DO UPDATE SET region = EXCLUDED.owner_id RETURNING id;
UPDATE sessions SET created_at = name + 3, id = CASE WHEN score > 486 THEN 'big' ELSE 'small' END WHERE id = $1 AND owner_id <> ALL (ARRAY[3, 9, 1]);
WITH recent AS (SELECT * FROM products WHERE score > current_date - 59), ranked AS (
  SELECT *, row_number() OVER (PARTITION BY score ORDER BY score DESC) AS rn FROM recent)
SELECT r.*, (SELECT max(name) FROM products x WHERE x.id = r.id) FROM ranked r WHERE rn <= 0
UNION ALL SELECT * FROM customers WHERE EXISTS (SELECT 1 FROM products WHERE status = $2);
DELETE FROM products USING orders WHERE orders.created_at = orders.id AND orders.id < now() - interval '1 year' RETURNING *;
SELECT a.id, b.score AS "B Col", count(*) FILTER (WHERE a.id > 471) AS n, coalesce(sum(a.region), 0)::numeric(12, 2)
  FROM orders AS a LEFT JOIN invoices b ON b.id = a.owner_id AND b.created_at IS NOT NULL
  WHERE a.created_at BETWEEN 94 AND 950 AND a.name IN ('x', 'y', 'z') AND NOT a.amount LIKE 'p%'
  GROUP BY 1, 2 HAVING count(*) > 8 ORDER BY n DESC, 1 LIMIT 64 OFFSET 520;
INSERT INTO products (id, name, owner_id) VALUES (793781, 'text 572829', now() + interval '18 days'), (70460, E'it''s\n', NULL)
  ON CONFLICT (id) DO UPDATE SET status = EXCLUDED.region RETURNING id;
UPDATE invoices SET owner_id = amount + 69, score = CASE WHEN created_at > 308 THEN 'big' ELSE 'small' END WHERE id = $1 AND amount <> ALL (ARRAY[9, 1, 9]);
WITH recent AS (SELECT * FROM events WHERE score > current_date - 59), ranked AS (
  SELECT *, row_number() OVER (PARTITION BY created_at ORDER BY id DESC) AS rn FROM recent)
SELECT r.*, (SELECT max(id) FROM orders x WHERE x.id = r.id) FROM ranked r WHERE rn <= 5
UNION ALL SELECT * FROM events WHERE EXISTS (SELECT 1 FROM customers WHERE created_at = $2);
DELETE FROM invoices USING sessions WHERE orders.created_at = orders.id AND orders.score < now() - interval '1 year' RETURNING *;
SELECT a.score, b.status AS "B Col", count(*) FILTER (WHERE a.region > 494) AS n, coalesce(sum(a.amount), 0)::numeric(12, 2)
  FROM customers AS a LEFT JOIN events b ON b.id = a.created_at AND b.id IS NOT NULL
  WHERE a.amount BETWEEN 10 AND 995 AND a.score IN ('x', 'y', 'z') AND NOT a.score LIKE 'p%'
  GROUP BY 1, 2 HAVING count(*) > 2 ORDER BY n DESC, 1 LIMIT 28 OFFSET 800;
INSERT INTO events (score, status, created_at) VALUES (671861, 'text 480280', now() + interval '21 days'), (86799, E'it''s\n', NULL)
  ON CONFLICT (id) DO UPDATE SET owner_id = EXCLUDED.owner_id RETURNING id;
UPDATE invoices SET score = owner_id + 28, name = CASE WHEN name > 631 THEN 'big' ELSE 'small' END WHERE id = $1 AND name <> ALL (ARRAY[9, 0, 6]);
WITH recent AS (SELECT * FROM products WHERE created_at > current_date - 70), ranked AS (
  SELECT *, row_number() OVER (PARTITION BY score ORDER BY owner_id DESC) AS rn FROM recent)
SELECT r.*, (SELECT max(id) FROM sessions x WHERE x.id = r.id) FROM ranked r WHERE rn <= 7
UNION ALL SELECT * FROM customers WHERE EXISTS (SELECT 1 FROM customers WHERE created_at = $2);
DELETE FROM orders USING products WHERE orders.owner_id = orders.id AND orders.region < now() - interval '1 year' RETURNING *;
SELECT a.amount, b.name AS "B Col", count(*) FILTER (WHERE a.owner_id > 623) AS n, coalesce(sum(a.name), 0)::numeric(12, 2)
  FROM sessions AS a LEFT JOIN events b ON b.id = a.score AND b.status IS NOT NULL
  WHERE a.id BETWEEN 76 AND 1015 AND a.amount IN ('x', 'y', 'z') AND NOT a.owner_id LIKE 'p%'
  GROUP BY 1, 2 HAVING count(*) > 6 ORDER BY n DESC, 1 LIMIT 21 OFFSET 898;
INSERT INTO invoices (owner_id, owner_id, name) VALUES (224485, 'text 193399', now() + interval '16 days'), (10295, E'it''s\n', NULL)
  ON CONFLICT (id) DO UPDATE SET created_at = EXCLUDED.status RETURNING id;
UPDATE customers SET status = region + 96, owner_id = CASE WHEN name > 608 THEN 'big' ELSE 'small' END WHERE id = $1 AND id <> ALL (ARRAY[1, 1, 0]);
WITH recent AS (SELECT * FROM orders WHERE created_at > current_date - 51), ranked AS (
  SELECT *, row_number() OVER (PARTITION BY id ORDER BY owner_id DESC) AS rn FROM recent)
SELECT r.*, (SELECT max(id) FROM invoices x WHERE x.id = r.id) FROM ranked r WHERE rn <= 2
UNION ALL SELECT * FROM sessions WHERE EXISTS (SELECT 1 FROM products WHERE region = $2);
DELETE FROM sessions USING invoices WHERE orders.amount = orders.id AND orders.amount < now() - interval '1 year' RETURNING *;
SELECT a.id, b.owner_id AS "B Col", count(*) FILTER (WHERE a.amount > 334) AS n, coalesce(sum(a.id), 0)::numeric(12, 2)
  FROM events AS a LEFT JOIN customers b ON b.id = a.created_at AND b.score IS NOT NULL
  WHERE a.id BETWEEN 18 AND 564 AND a.created_at IN ('x', 'y', 'z') AND NOT a.status LIKE 'p%'
  GROUP BY 1, 2 HAVING count(*) > 1 ORDER BY n DESC, 1 LIMIT 46 OFFSET 498;
INSERT INTO events (region, region, created_at) VALUES (820019, 'text 609123', now() + interval '10 days'), (779708, E'it''s\n', NULL)
  ON CONFLICT (id) DO UPDATE SET amount = EXCLUDED.status RETURNING id;
UPDATE products SET score = region + 22, status = CASE WHEN owner_id > 470 THEN 'big' ELSE 'small' END WHERE id = $1 AND owner_id <> ALL (ARRAY[9, 3, 9]);
WITH recent AS (SELECT * FROM customers WHERE amount > current_date - 56), ranked AS (
  SELECT *, row_number() OVER (PARTITION BY amount ORDER BY id DESC) AS rn FROM recent)
SELECT r.*, (SELECT max(owner_id) FROM events x WHERE x.id = r.id) FROM ranked r WHERE rn <= 8
UNION ALL SELECT * FROM products WHERE EXISTS (SELECT 1 FROM events WHERE name = $2);
DELETE FROM events USING sessions WHERE orders.score = orders.id AND orders.owner_id < now() - interval '1 year' RETURNING *;
SELECT a.score, b.owner_id AS "B Col", count(*) FILTER (WHERE a.status > 733) AS n, coalesce(sum(a.created_at), 0)::numeric(12, 2)
  FROM products AS a LEFT JOIN sessions b ON b.id = a.amount AND b.amount IS NOT NULL
  WHERE a.amount BETWEEN 56 AND 801 AND a.name IN ('x', 'y', 'z') AND NOT a.score LIKE 'p%'
  GROUP BY 1, 2 HAVING count(*) > 6 ORDER BY n DESC, 1 LIMIT 69 OFFSET 960;
INSERT INTO products (owner_id, score, created_at) VALUES (427120, 'text 760134', now() + interval '4 days'), (663779, E'it''s\n', NULL)
  ON CONFLICT (id) DO UPDATE SET created_at = EXCLUDED.id RETURNING id;
UPDATE customers SET amount = amount + 67, status = CASE WHEN amount > 123 THEN 'big' ELSE 'small' END WHERE id = $1 AND status <> ALL (ARRAY[2, 7, 8]);
WITH recent AS (SELECT * FROM products WHERE region > current_date - 41), ranked AS (
  SELECT *, row_number() OVER (PARTITION BY created_at ORDER BY created_at DESC) AS rn FROM recent)
SELECT r.*, (SELECT max(region) FROM invoices x WHERE x.id = r.id) FROM ranked r WHERE rn <= 9
UNION ALL SELECT * FROM orders WHERE EXISTS (SELECT 1 FROM sessions WHERE region = $2);
DELETE FROM invoices USING products WHERE orders.created_at = orders.id AND orders.score < now() - interval '1 year' RETURNING *;
SELECT a.score, b.region AS "B Col", count(*) FILTER (WHERE a.id > 195) AS n, coalesce(sum(a.amount), 0)::numeric(12, 2)
  FROM orders AS a LEFT JOIN customers b ON b.id = a.created_at AND b.name IS NOT NULL
  WHERE a.status BETWEEN 11 AND 439 AND a.amount IN ('x', 'y', 'z') AND NOT a.status LIKE 'p%'
  GROUP BY 1, 2 HAVING count(*) > 5 ORDER BY n DESC, 1 LIMIT 8 OFFSET 122;
INSERT INTO products (name, score, created_at) VALUES (410255, 'text 658023', now() + interval '24 days'), (793883, E'it''s\n', NULL)
  ON CONFLICT (id) DO UPDATE SET score = EXCLUDED.score RETURNING id;
UPDATE products SET owner_id = region + 80, name = CASE WHEN owner_id > 575 THEN 'big' ELSE 'small' END WHERE id = $1 AND status <> ALL (ARRAY[4, 6, 3]);
WITH recent AS (SELECT * FROM invoices WHERE amount > current_date - 87), ranked AS (
  SELECT *, row_number() OVER (PARTITION BY status ORDER BY status DESC) AS rn FROM recent)
SELECT r.*, (SELECT max(score) FROM products x WHERE x.id = r.id) FROM ranked r WHERE rn <= 0
UNION ALL SELECT * FROM sessions WHERE EXISTS (SELECT 1 FROM invoices WHERE region = $2);
DELETE FROM orders USING products WHERE orders.status = orders.id AND orders.score < now() - interval '1 year' RETURNING *;
SELECT a.owner_id, b.score AS "B Col", count(*) FILTER (WHERE a.created_at > 76) AS n, coalesce(sum(a.region), 0)::numeric(12, 2)
  FROM events AS a LEFT JOIN sessions b ON b.id = a.owner_id AND b.status IS NOT NULL
  WHERE a.name BETWEEN 54 AND 1041 AND a.created_at IN ('x', 'y', 'z') AND NOT a.created_at LIKE 'p%'
  GROUP BY 1, 2 HAVING count(*) > 6 ORDER BY n DESC, 1 LIMIT 98 OFFSET 489;
INSERT INTO invoices (status, created_at, id) VALUES (368183, 'text 249336', now() + interval '3 days'), (371002, E'it''s\n', NULL)
  ON CONFLICT (id) DO UPDATE SET amount = EXCLUDED.id RETURNING id;
UPDATE events SET region = status + 34, id = CASE WHEN amount > 63 THEN 'big' ELSE 'small' END WHERE id = $1 AND status <> ALL (ARRAY[0, 2, 7]);
WITH recent AS (SELECT * FROM sessions WHERE created_at > current_date - 12), ranked AS (
  SELECT *, row_number() OVER (PARTITION BY owner_id ORDER BY id DESC) AS rn FROM recent)
SELECT r.*, (SELECT max(id) FROM invoices x WHERE x.id = r.id) FROM ranked r WHERE rn <= 8
UNION ALL SELECT * FROM sessions WHERE EXISTS (SELECT 1 FROM products WHERE score = $2);
DELETE FROM events USING events WHERE orders.amount = orders.id AND orders.created_at < now() - interval '1 year' RETURNING *;
SELECT a.region, b.created_at AS "B Col", count(*) FILTER (WHERE a.score > 869) AS n, coalesce(sum(a.id), 0)::numeric(12, 2)
  FROM customers AS a LEFT JOIN events b ON b.id = a.amount AND b.amount IS NOT NULL
  WHERE a.region BETWEEN 65 AND 540 AND a.amount IN ('x', 'y', 'z') AND NOT a.status LIKE 'p%'
  GROUP BY 1, 2 HAVING count(*) > 0 ORDER BY n DESC, 1 LIMIT 38 OFFSET 473;
INSERT INTO customers (created_at, score, status) VALUES (10863, 'text 805855', now() + interval '18 days'), (510112, E'it''s\n', NULL)
  ON CONFLICT (id) DO UPDATE SET amount = EXCLUDED.amount RETURNING id;
UPDATE invoices SET created_at = id + 30, owner_id = CASE WHEN status > 777 THEN 'big' ELSE 'small' END WHERE id = $1 AND region <> ALL (ARRAY[0, 4, 8]);
WITH recent AS (SELECT * FROM invoices WHERE created_at > current_date - 27), ranked AS (
  SELECT *, row_number() OVER (PARTITION BY owner_id ORDER BY owner_id DESC) AS rn FROM recent)
SELECT r.*, (SELECT max(status) FROM orders x WHERE x.id = r.id) FROM ranked r WHERE rn <= 4
UNION ALL SELECT * FROM orders WHERE EXISTS (SELECT 1 FROM orders WHERE region = $2);
DELETE FROM customers USING invoices WHERE orders.owner_id = orders.id AND orders.region < now() - interval '1 year' RETURNING *;
SELECT a.owner_id, b.amount AS "B Col", count(*) FILTER (WHERE a.id > 556) AS n, coalesce(sum(a.owner_id), 0)::numeric(12, 2)
  FROM events AS a LEFT JOIN products b ON b.id = a.created_at AND b.owner_id IS NOT NULL
  WHERE a.status BETWEEN 95 AND 1087 AND a.amount IN ('x', 'y', 'z') AND NOT a.created_at LIKE 'p%'
  GROUP BY 1, 2 HAVING count(*) > 9 ORDER BY n DESC, 1 LIMIT 29 OFFSET 885;
INSERT INTO sessions (id, name, created_at) VALUES (811386, 'text 213528', now() + interval '9 days'), (554584, E'it''s\n', NULL)
  ON CONFLICT (id) DO UPDATE SET owner_id = EXCLUDED.owner_id RETURNING id;
UPDATE invoices SET name = score + 1, name = CASE WHEN name > 980 THEN 'big' ELSE 'small' END WHERE id = $1 AND owner_id <> ALL (ARRAY[7, 0, 4]);
WITH recent AS (SELECT * FROM invoices WHERE id > current_date - 63), ranked AS (
  SELECT *, row_number() OVER (PARTITION BY owner_id ORDER BY amount DESC) AS rn FROM recent)
SELECT r.*, (SELECT max(status) FROM products x WHERE x.id = r.id) FROM ranked r WHERE rn <= 3
UNION ALL SELECT * FROM orders WHERE EXISTS (SELECT 1 FROM products WHERE score = $2);
DELETE FROM orders USING orders WHERE orders.score = orders.id AND orders.amount < now() - interval '1 year' RETURNING *;
SELECT a.owner_id, b.region AS "B Col", count(*) FILTER (WHERE a.amount > 965) AS n, coalesce(sum(a.name), 0)::numeric(12, 2)
  FROM sessions AS a LEFT JOIN products b ON b.id = a.name AND b.id IS NOT NULL
  WHERE a.id BETWEEN 7 AND 162 AND a.id IN ('x', 'y', 'z') AND NOT a.created_at LIKE 'p%'
  GROUP BY 1, 2 HAVING count(*) > 5 ORDER BY n DESC, 1 LIMIT 74 OFFSET 124;
INSERT INTO invoices (created_at, name, amount) VALUES (473815, 'text 898521', now() + interval '0 days'), (237321, E'it''s\n', NULL)
  ON CONFLICT (id) DO UPDATE SET status = EXCLUDED.status RETURNING id;
UPDATE products SET name = name + 92, amount = CASE WHEN created_at > 338 THEN 'big' ELSE 'small' END WHERE id = $1 AND status <> ALL (ARRAY[2, 6, 4]);
WITH recent AS (SELECT * FROM orders WHERE created_at > current_date - 83), ranked AS (
  SELECT *, row_number() OVER (PARTITION BY region ORDER BY status DESC) AS rn FROM recent)
SELECT r.*, (SELECT max(id) FROM orders x WHERE x.id = r.id) FROM ranked r WHERE rn <= 6
UNION ALL SELECT * FROM products WHERE EXISTS (SELECT 1 FROM events WHERE name = $2);
DELETE FROM events USING sessions WHERE orders.name = orders.id AND orders.owner_id < now() - interval '1 year' RETURNING *;
SELECT a.owner_id, b.score AS "B Col", count(*) FILTER (WHERE a.status > 551) AS n, coalesce(sum(a.status), 0)::numeric(12, 2)
  FROM orders AS a LEFT JOIN sessions b ON b.id = a.amount AND b.name IS NOT NULL
  WHERE a.created_at BETWEEN 3 AND 520 AND a.region IN ('x', 'y', 'z') AND NOT a.created_at LIKE 'p%'
  GROUP BY 1, 2 HAVING count(*) > 8 ORDER BY n DESC, 1 LIMIT 35 OFFSET 54;
INSERT INTO orders (status, name, name) VALUES (951368, 'text 4464', now() + interval '16 days'), (414160, E'it''s\n', NULL)
  ON CONFLICT (id) DO UPDATE SET id = EXCLUDED.amount RETURNING id;
UPDATE events SET created_at = owner_id + 79, status = CASE WHEN id > 8 THEN 'big' ELSE 'small' END WHERE id = $1 AND id <> ALL (ARRAY[1, 9, 7]);
WITH recent AS (SELECT * FROM orders WHERE score > current_date - 54), ranked AS (
  SELECT *, row_number() OVER (PARTITION BY name ORDER BY owner_id DESC) AS rn FROM recent)
SELECT r.*, (SELECT max(amount) FROM customers x WHERE x.id = r.id) FROM ranked r WHERE rn <= 2
UNION ALL SELECT * FROM orders WHERE EXISTS (SELECT 1 FROM invoices WHERE region = $2);
DELETE FROM sessions USING customers WHERE orders.region = orders.id AND orders.name < now() - interval '1 year' RETURNING *;
SELECT a.created_at, b.region AS "B Col", count(*) FILTER (WHERE a.owner_id > 464) AS n, coalesce(sum(a.status), 0)::numeric(12, 2)
  FROM events AS a LEFT JOIN sessions b ON b.id = a.score AND b.status IS NOT NULL
  WHERE a.id BETWEEN 30 AND 583 AND a.created_at IN ('x', 'y', 'z') AND NOT a.owner_id LIKE 'p%'
  GROUP BY 1, 2 HAVING count(*) > 2 ORDER BY n DESC, 1 LIMIT 72 OFFSET 663;
INSERT INTO products (score, created_at, owner_id) VALUES (466448, 'text 958148', now() + interval '1 days'), (80969, E'it''s\n', NULL)
  ON CONFLICT (id) DO UPDATE SET amount = EXCLUDED.name RETURNING id;
UPDATE events SET amount = status + 42, status = CASE WHEN amount > 115 THEN 'big' ELSE 'small' END WHERE id = $1 AND created_at <> ALL (ARRAY[5, 0, 9]);
WITH recent AS (SELECT * FROM orders WHERE score > current_date - 68), ranked AS (
  SELECT *, row_number() OVER (PARTITION BY region ORDER BY name DESC) AS rn FROM recent)
SELECT r.*, (SELECT max(owner_id) FROM products x WHERE x.id = r.id) FROM ranked r WHERE rn <= 9
UNION ALL SELECT * FROM sessions WHERE EXISTS (SELECT 1 FROM sessions WHERE owner_id = $2);
DELETE FROM products USING events WHERE orders.created_at = orders.id AND orders.id < now() - interval '1 year' RETURNING *;
SELECT a.amount, b.region AS "B Col", count(*) FILTER (WHERE a.status > 799) AS n, coalesce(sum(a.id), 0)::numeric(12, 2)
  FROM customers AS a LEFT JOIN sessions b ON b.id = a.region AND b.name IS NOT NULL
  WHERE a.status BETWEEN 45 AND 620 AND a.owner_id IN ('x', 'y', 'z') AND NOT a.score LIKE 'p%'
  GROUP BY 1, 2 HAVING count(*) > 0 ORDER BY n DESC, 1 LIMIT 69 OFFSET 354;
INSERT INTO customers (score, id, amount) VALUES (440809, 'text 16675', now() + interval '27 days'), (526264, E'it''s\n', NULL)
  ON CONFLICT (id) DO UPDATE SET id = EXCLUDED.region RETURNING id;
UPDATE products SET name = region + 84, score = CASE WHEN region > 556 THEN 'big' ELSE 'small' END WHERE id = $1 AND id <> ALL (ARRAY[4, 1, 8]);
WITH recent AS (SELECT * FROM orders WHERE id > current_date - 66), ranked AS (
  SELECT *, row_number() OVER (PARTITION BY score ORDER BY owner_id DESC) AS rn FROM recent)
SELECT r.*, (SELECT max(amount) FROM customers x WHERE x.id = r.id) FROM ranked r WHERE rn <= 4
UNION ALL SELECT * FROM events WHERE EXISTS (SELECT 1 FROM sessions WHERE owner_id = $2);
DELETE FROM invoices USING orders WHERE orders.region = orders.id AND orders.created_at < now() - interval '1 year' RETURNING *;
SELECT a.score, b.score AS "B Col", count(*) FILTER (WHERE a.id > 461) AS n, coalesce(sum(a.amount), 0)::numeric(12, 2)
  FROM invoices AS a LEFT JOIN events b ON b.id = a.name AND b.status IS NOT NULL
  WHERE a.name BETWEEN 88 AND 913 AND a.name IN ('x', 'y', 'z') AND NOT a.name LIKE 'p%'
  GROUP BY 1, 2 HAVING count(*) > 3 ORDER BY n DESC, 1 LIMIT 77 OFFSET 640;
INSERT INTO events (status, owner_id, name) VALUES (396133, 'text 413970', now() + interval '21 days'), (971465, E'it''s\n', NULL)
  ON CONFLICT (id) DO UPDATE SET id = EXCLUDED.name RETURNING id;
UPDATE invoices SET owner_id = amount + 91, amount = CASE WHEN status > 215 THEN 'big' ELSE 'small' END WHERE id = $1 AND status <> ALL (ARRAY[5, 0, 1]);
WITH recent AS (SELECT * FROM invoices WHERE amount > current_date - 72), ranked AS (
  SELECT *, row_number() OVER (PARTITION BY amount ORDER BY score DESC) AS rn FROM recent)
SELECT r.*, (SELECT max(status) FROM events x WHERE x.id = r.id) FROM ranked r WHERE rn <= 1
UNION ALL SELECT * FROM orders WHERE EXISTS (SELECT 1 FROM products WHERE id = $2);
DELETE FROM sessions USING events WHERE orders.name = orders.id AND orders.name < now() - interval '1 year' RETURNING *;
SELECT a.status, b.score AS "B Col", count(*) FILTER (WHERE a.owner_id > 719) AS n, coalesce(sum(a.name), 0)::numeric(12, 2)
  FROM sessions AS a LEFT JOIN sessions b ON b.id = a.created_at AND b.name IS NOT NULL
  WHERE a.owner_id BETWEEN 43 AND 507 AND a.status IN ('x', 'y', 'z') AND NOT a.owner_id LIKE 'p%'
  GROUP BY 1, 2 HAVING count(*) > 4 ORDER BY n DESC, 1 LIMIT 55 OFFSET 368;
INSERT INTO products (id, status, region) VALUES (591035, 'text 155061', now() + interval '9 days'), (193175, E'it''s\n', NULL)
  ON CONFLICT (id) DO UPDATE SET status = EXCLUDED.status RETURNING id;
UPDATE events SET region = id + 11, score = CASE WHEN amount > 359 THEN 'big' ELSE 'small' END WHERE id = $1 AND region <> ALL (ARRAY[3, 3, 1]);
WITH recent AS (SELECT * FROM events WHERE id > current_date - 57), ranked AS (
  SELECT *, row_number() OVER (PARTITION BY status ORDER BY name DESC) AS rn FROM recent)
SELECT r.*, (SELECT max(region) FROM invoices x WHERE x.id = r.id) FROM ranked r WHERE rn <= 8
UNION ALL SELECT * FROM orders WHERE EXISTS (SELECT 1 FROM events WHERE amount = $2);
DELETE FROM customers USING customers WHERE orders.created_at = orders.id AND orders.id < now() - interval '1 year' RETURNING *;
SELECT a.status, b.owner_id AS "B Col", count(*) FILTER (WHERE a.id > 926) AS n, coalesce(sum(a.region), 0)::numeric(12, 2)
  FROM events AS a LEFT JOIN sessions b ON b.id = a.name AND b.name IS NOT NULL
  WHERE a.region BETWEEN 3 AND 322 AND a.name IN ('x', 'y', 'z') AND NOT a.owner_id LIKE 'p%'
  GROUP BY 1, 2 HAVING count(*) > 6 ORDER BY n DESC, 1 LIMIT 5 OFFSET 549;
INSERT INTO invoices (region, owner_id, status) VALUES (649499, 'text 672887', now() + interval '15 days'), (527349, E'it''s\n', NULL)
  ON CONFLICT (id) DO UPDATE SET owner_id = EXCLUDED.id RETURNING id;
UPDATE orders SET created_at = amount + 82, id = CASE WHEN amount > 250 THEN 'big' ELSE 'small' END WHERE id = $1 AND name <> ALL (ARRAY[7, 6, 3]);
WITH recent AS (SELECT * FROM sessions WHERE score > current_date - 85), ranked AS (
  SELECT *, row_number() OVER (PARTITION BY id ORDER BY created_at DESC) AS rn FROM recent)
SELECT r.*, (SELECT max(score) FROM invoices x WHERE x.id = r.id) FROM ranked r WHERE rn <= 3
UNION ALL SELECT * FROM orders WHERE EXISTS (SELECT 1 FROM sessions WHERE status = $2);
DELETE FROM customers USING customers WHERE orders.owner_id = orders.id AND orders.amount < now() - interval '1 year' RETURNING *;
SELECT a.id, b.id AS "B Col", count(*) FILTER (WHERE a.id > 60) AS n, coalesce(sum(a.name), 0)::numeric(12, 2)
  FROM invoices AS a LEFT JOIN customers b ON b.id = a.name AND b.name IS NOT NULL
  WHERE a.id BETWEEN 67 AND 305 AND a.score IN ('x', 'y', 'z') AND NOT a.region LIKE 'p%'
  GROUP BY 1, 2 HAVING count(*) > 3 ORDER BY n DESC, 1 LIMIT 5 OFFSET 314;
INSERT INTO events (name, created_at, amount) VALUES (652081, 'text 36230', now() + interval '11 days'), (227731, E'it''s\n', NULL)
  ON CONFLICT (id) DO UPDATE SET id = EXCLUDED.score RETURNING id;
UPDATE products SET score = amount + 13, owner_id = CASE WHEN id > 957 THEN 'big' ELSE 'small' END WHERE id = $1 AND id <> ALL (ARRAY[3, 3, 9]);
WITH recent AS (SELECT * FROM products WHERE amount > current_date - 20), ranked AS (
  SELECT *, row_number() OVER (PARTITION BY name ORDER BY score DESC) AS rn FROM recent)
SELECT r.*, (SELECT max(status) FROM customers x WHERE x.id = r.id) FROM ranked r WHERE rn <= 4
UNION ALL SELECT * FROM products WHERE EXISTS (SELECT 1 FROM events WHERE score = $2);
DELETE FROM orders USING products WHERE orders.score = orders.id AND orders.score < now() - interval '1 year' RETURNING *;
SELECT a.region, b.region AS "B Col", count(*) FILTER (WHERE a.score > 17) AS n, coalesce(sum(a.status), 0)::numeric(12, 2)
  FROM orders AS a LEFT JOIN customers b ON b.id = a.region AND b.created_at IS NOT NULL
  WHERE a.score BETWEEN 6 AND 513 AND a.id IN ('x', 'y', 'z') AND NOT a.status LIKE 'p%'
  GROUP BY 1, 2 HAVING count(*) > 9 ORDER BY n DESC, 1 LIMIT 87 OFFSET 812;
INSERT INTO orders (status, created_at, amount) VALUES (31584, 'text 19677', now() + interval '13 days'), (591773, E'it''s\n', NULL)
  ON CONFLICT (id) DO UPDATE SET id = EXCLUDED.created_at RETURNING id;
UPDATE products SET id = amount + 17, score = CASE WHEN owner_id > 381 THEN 'big' ELSE 'small' END WHERE id = $1 AND status <> ALL (ARRAY[2, 9, 1]);
WITH recent AS (SELECT * FROM invoices WHERE owner_id > current_date - 51), ranked AS (
  SELECT *, row_number() OVER (PARTITION BY amount ORDER BY region DESC) AS rn FROM recent)
SELECT r.*, (SELECT max(region) FROM products x WHERE x.id = r.id) FROM ranked r WHERE rn <= 7
UNION ALL SELECT * FROM sessions WHERE EXISTS (SELECT 1 FROM invoices WHERE region = $2);
DELETE FROM customers USING customers WHERE orders.amount = orders.id AND orders.name < now() - interval '1 year' RETURNING *;
SELECT a.created_at, b.id AS "B Col", count(*) FILTER (WHERE a.status > 197) AS n, coalesce(sum(a.owner_id), 0)::numeric(12, 2)
  FROM invoices AS a LEFT JOIN invoices b ON b.id = a.status AND b.score IS NOT NULL
  WHERE a.created_at BETWEEN 31 AND 903 AND a.region IN ('x', 'y', 'z') AND NOT a.region LIKE 'p%'
  GROUP BY 1, 2 HAVING count(*) > 3 ORDER BY n DESC, 1 LIMIT 15 OFFSET 264;
INSERT INTO orders (amount, score, id) VALUES (435117, 'text 531020', now() + interval '11 days'), (194935, E'it''s\n', NULL)
  ON CONFLICT (id) DO UPDATE SET owner_id = EXCLUDED.created_at RETURNING id;
UPDATE invoices SET score = id + 20, name = CASE WHEN status > 766 THEN 'big' ELSE 'small' END WHERE id = $1 AND created_at <> ALL (ARRAY[9, 1, 6]);
WITH recent AS (SELECT * FROM sessions WHERE status > current_date - 60), ranked AS (
  SELECT *, row_number() OVER (PARTITION BY score ORDER BY name DESC) AS rn FROM recent)
SELECT r.*, (SELECT max(region) FROM products x WHERE x.id = r.id) FROM ranked r WHERE rn <= 7
UNION ALL SELECT * FROM orders WHERE EXISTS (SELECT 1 FROM orders WHERE region = $2);
DELETE FROM invoices USING events WHERE orders.amount = orders.id AND orders.name < now() - interval '1 year' RETURNING *;
SELECT a.region, b.region AS "B Col", count(*) FILTER (WHERE a.status > 480) AS n, coalesce(sum(a.owner_id), 0)::numeric(12, 2)
  FROM orders AS a LEFT JOIN invoices b ON b.id = a.status AND b.id IS NOT NULL
  WHERE a.created_at BETWEEN 38 AND 547 AND a.id IN ('x', 'y', 'z') AND NOT a.created_at LIKE 'p%'
  GROUP BY 1, 2 HAVING count(*) > 7 ORDER BY n DESC, 1 LIMIT 36 OFFSET 421;
INSERT INTO orders (name, amount, id) VALUES (937101, 'text 599220', now() + interval '0 days'), (333774, E'it''s\n', NULL)
  ON CONFLICT (id) DO UPDATE SET name = EXCLUDED.created_at RETURNING id;
UPDATE events SET owner_id = status + 10, created_at = CASE WHEN amount > 83 THEN 'big' ELSE 'small' END WHERE id = $1 AND score <> ALL (ARRAY[6, 2, 4]);
WITH recent AS (SELECT * FROM orders WHERE region > current_date - 52), ranked AS (
  SELECT *, row_number() OVER (PARTITION BY status ORDER BY name DESC) AS rn FROM recent)
SELECT r.*, (SELECT max(status) FROM sessions x WHERE x.id = r.id) FROM ranked r WHERE rn <= 6
UNION ALL SELECT * FROM events WHERE EXISTS (SELECT 1 FROM orders WHERE amount = $2);
DELETE FROM orders USING products WHERE orders.region = orders.id AND orders.owner_id < now() - interval '1 year' RETURNING *;
SELECT a.region, b.status AS "B Col", count(*) FILTER (WHERE a.id > 311) AS n, coalesce(sum(a.owner_id), 0)::numeric(12, 2)
  FROM products AS a LEFT JOIN events b ON b.id = a.owner_id AND b.owner_id IS NOT NULL
  WHERE a.owner_id BETWEEN 57 AND 419 AND a.amount IN ('x', 'y', 'z') AND NOT a.created_at LIKE 'p%'
  GROUP BY 1, 2 HAVING count(*) > 2 ORDER BY n DESC, 1 LIMIT 5 OFFSET 19;
INSERT INTO invoices (score, amount, region) VALUES (378384, 'text 250011', now() + interval '2 days'), (629885, E'it''s\n', NULL)
  ON CONFLICT (id) DO UPDATE SET created_at = EXCLUDED.id RETURNING id;
UPDATE products SET region = owner_id + 12, owner_id = CASE WHEN status > 941 THEN 'big' ELSE 'small' END WHERE id = $1 AND created_at <> ALL (ARRAY[7, 1, 4]);
WITH recent AS (SELECT * FROM products WHERE owner_id > current_date - 33), ranked AS (
  SELECT *, row_number() OVER (PARTITION BY id ORDER BY score DESC) AS rn FROM recent)
SELECT r.*, (SELECT max(name) FROM sessions x WHERE x.id = r.id) FROM ranked r WHERE rn <= 8
UNION ALL SELECT * FROM customers WHERE EXISTS (SELECT 1 FROM sessions WHERE score = $2);
DELETE FROM customers USING customers WHERE orders.name = orders.id AND orders.created_at < now() - interval '1 year' RETURNING *;
SELECT a.amount, b.score AS "B Col", count(*) FILTER (WHERE a.amount > 649) AS n, coalesce(sum(a.score), 0)::numeric(12, 2)
  FROM invoices AS a LEFT JOIN sessions b ON b.id = a.name AND b.owner_id IS NOT NULL
  WHERE a.name BETWEEN 67 AND 1008 AND a.amount IN ('x', 'y', 'z') AND NOT a.name LIKE 'p%'
  GROUP BY 1, 2 HAVING count(*) > 9 ORDER BY n DESC, 1 LIMIT 45 OFFSET 747;
INSERT INTO customers (score, score, status) VALUES (997708, 'text 51203', now() + interval '1 days'), (2637, E'it''s\n', NULL)
  ON CONFLICT (id) DO UPDATE SET created_at = EXCLUDED.owner_id RETURNING id;
UPDATE invoices SET created_at = amount + 51, region = CASE WHEN region > 850 THEN 'big' ELSE 'small' END WHERE id = $1 AND region <> ALL (ARRAY[4, 2, 9]);
WITH recent AS (SELECT * FROM events WHERE id > current_date - 75), ranked AS (
  SELECT *, row_number() OVER (PARTITION BY amount ORDER BY name DESC) AS rn FROM recent)
SELECT r.*, (SELECT max(amount) FROM customers x WHERE x.id = r.id) FROM ranked r WHERE rn <= 7
UNION ALL SELECT * FROM orders WHERE EXISTS (SELECT 1 FROM invoices WHERE id = $2);
DELETE FROM events USING customers WHERE orders.name = orders.id AND orders.created_at < now() - interval '1 year' RETURNING *;
SELECT a.created_at, b.name AS "B Col", count(*) FILTER (WHERE a.region > 645) AS n, coalesce(sum(a.name), 0)::numeric(12, 2)
  FROM events AS a LEFT JOIN invoices b ON b.id = a.region AND b.name IS NOT NULL
  WHERE a.created_at BETWEEN 63 AND 964 AND a.amount IN ('x', 'y', 'z') AND NOT a.created_at LIKE 'p%'
  GROUP BY 1, 2 HAVING count(*) > 5 ORDER BY n DESC, 1 LIMIT 51 OFFSET 350;
INSERT INTO events (status, name, id) VALUES (31303, 'text 234054', now() + interval '10 days'), (620779, E'it''s\n', NULL)
  ON CONFLICT (id) DO UPDATE SET region = EXCLUDED.id RETURNING id;
UPDATE products SET name = status + 7, owner_id = CASE WHEN status > 844 THEN 'big' ELSE 'small' END WHERE id = $1 AND region <> ALL (ARRAY[0, 6, 4]);
WITH recent AS (SELECT * FROM orders WHERE name > current_date - 89), ranked AS (
  SELECT *, row_number() OVER (PARTITION BY amount ORDER BY amount DESC) AS rn FROM recent)
SELECT r.*, (SELECT max(status) FROM events x WHERE x.id = r.id) FROM ranked r WHERE rn <= 1
UNION ALL SELECT * FROM events WHERE EXISTS (SELECT 1 FROM sessions WHERE score = $2);
DELETE FROM orders USING events WHERE orders.owner_id = orders.id AND orders.owner_id < now() - interval '1 year' RETURNING *;
SELECT a.region, b.created_at AS "B Col", count(*) FILTER (WHERE a.status > 840) AS n, coalesce(sum(a.region), 0)::numeric(12, 2)
  FROM invoices AS a LEFT JOIN events b ON b.id = a.created_at AND b.id IS NOT NULL
  WHERE a.amount BETWEEN 69 AND 900 AND a.name IN ('x', 'y', 'z') AND NOT a.status LIKE 'p%'
  GROUP BY 1, 2 HAVING count(*) > 3 ORDER BY n DESC, 1 LIMIT 49 OFFSET 892;
INSERT INTO orders (created_at, score, status) VALUES (64438, 'text 512252', now() + interval '22 days'), (960304, E'it''s\n', NULL)
  ON CONFLICT (id) DO UPDATE SET id = EXCLUDED.status RETURNING id;
UPDATE sessions SET amount = region + 87, amount = CASE WHEN owner_id > 12 THEN 'big' ELSE 'small' END WHERE id = $1 AND status <> ALL (ARRAY[2, 8, 3]);
WITH recent AS (SELECT * FROM orders WHERE score > current_date - 68), ranked AS (
  SELECT *, row_number() OVER (PARTITION BY owner_id ORDER BY id DESC) AS rn FROM recent)
SELECT r.*, (SELECT max(name) FROM sessions x WHERE x.id = r.id) FROM ranked r WHERE rn <= 0
UNION ALL SELECT * FROM orders WHERE EXISTS (SELECT 1 FROM products WHERE name = $2);
DELETE FROM orders USING sessions WHERE orders.region = orders.id AND orders.id < now() - interval '1 year' RETURNING *;
SELECT a.region, b.name AS "B Col", count(*) FILTER (WHERE a.score > 860) AS n, coalesce(sum(a.created_at), 0)::numeric(12, 2)
  FROM events AS a LEFT JOIN products b ON b.id = a.created_at AND b.status IS NOT NULL
  WHERE a.created_at BETWEEN 35 AND 733 AND a.score IN ('x', 'y', 'z') AND NOT a.score LIKE 'p%'
  GROUP BY 1, 2 HAVING count(*) > 1 ORDER BY n DESC, 1 LIMIT 38 OFFSET 530;
INSERT INTO events (created_at, region, id) VALUES (170020, 'text 614429', now() + interval '8 days'), (592051, E'it''s\n', NULL)
  ON CONFLICT (id) DO UPDATE SET owner_id = EXCLUDED.status RETURNING id;
UPDATE sessions SET region = created_at + 83, status = CASE WHEN status > 488 THEN 'big' ELSE 'small' END WHERE id = $1 AND created_at <> ALL (ARRAY[5, 7, 2]);
WITH recent AS (SELECT * FROM sessions WHERE id > current_date - 50), ranked AS (
  SELECT *, row_number() OVER (PARTITION BY owner_id ORDER BY name DESC) AS rn FROM recent)
SELECT r.*, (SELECT max(score) FROM orders x WHERE x.id = r.id) FROM ranked r WHERE rn <= 8
UNION ALL SELECT * FROM sessions WHERE EXISTS (SELECT 1 FROM customers WHERE region = $2);
DELETE FROM sessions USING invoices WHERE orders.created_at = orders.id AND orders.region < now() - interval '1 year' RETURNING *;
SELECT a.owner_id, b.created_at AS "B Col", count(*) FILTER (WHERE a.amount > 131) AS n, coalesce(sum(a.score), 0)::numeric(12, 2)
  FROM invoices AS a LEFT JOIN sessions b ON b.id = a.name AND b.id IS NOT NULL
  WHERE a.name BETWEEN 99 AND 224 AND a.region IN ('x', 'y', 'z') AND NOT a.score LIKE 'p%'
  GROUP BY 1, 2 HAVING count(*) > 3 ORDER BY n DESC, 1 LIMIT 26 OFFSET 788;
INSERT INTO sessions (amount, score, name) VALUES (409760, 'text 90431', now() + interval '20 days'), (796681, E'it''s\n', NULL)
  ON CONFLICT (id) DO UPDATE SET owner_id = EXCLUDED.score RETURNING id;
UPDATE orders SET owner_id = name + 68, amount = CASE WHEN owner_id > 243 THEN 'big' ELSE 'small' END WHERE id = $1 AND region <> ALL (ARRAY[2, 7, 3]);
WITH recent AS (SELECT * FROM sessions WHERE region > current_date - 47), ranked AS (
  SELECT *, row_number() OVER (PARTITION BY amount ORDER BY created_at DESC) AS rn FROM recent)
SELECT r.*, (SELECT max(region) FROM invoices x WHERE x.id = r.id) FROM ranked r WHERE rn <= 2
UNION ALL SELECT * FROM invoices WHERE EXISTS (SELECT 1 FROM invoices WHERE owner_id = $2);
DELETE FROM orders USING sessions WHERE orders.id = orders.id AND orders.id < now() - interval '1 year' RETURNING *;
SELECT a.amount, b.owner_id AS "B Col", count(*) FILTER (WHERE a.id > 870) AS n, coalesce(sum(a.name), 0)::numeric(12, 2)
  FROM sessions AS a LEFT JOIN orders b ON b.id = a.region AND b.owner_id IS NOT NULL
  WHERE a.amount BETWEEN 22 AND 509 AND a.region IN ('x', 'y', 'z') AND NOT a.region LIKE 'p%'
  GROUP BY 1, 2 HAVING count(*) > 9 ORDER BY n DESC, 1 LIMIT 99 OFFSET 967;
INSERT INTO customers (name, created_at, status) VALUES (885803, 'text 288744', now() + interval '20 days'), (11667, E'it''s\n', NULL)
  ON CONFLICT (id) DO UPDATE SET amount = EXCLUDED.created_at RETURNING id;
UPDATE orders SET owner_id = name + 16, region = CASE WHEN owner_id > 478 THEN 'big' ELSE 'small' END WHERE id = $1 AND score <> ALL (ARRAY[7, 5, 5]);
WITH recent AS (SELECT * FROM products WHERE score > current_date - 88), ranked AS (
  SELECT *, row_number() OVER (PARTITION BY name ORDER BY owner_id DESC) AS rn FROM recent)
SELECT r.*, (SELECT max(id) FROM orders x WHERE x.id = r.id) FROM ranked r WHERE rn <= 5
UNION ALL SELECT * FROM orders WHERE EXISTS (SELECT 1 FROM orders WHERE id = $2);
DELETE FROM customers USING sessions WHERE orders.status = orders.id AND orders.owner_id < now() - interval '1 year' RETURNING *;
SELECT a.name, b.name AS "B Col", count(*) FILTER (WHERE a.score > 434) AS n, coalesce(sum(a.name), 0)::numeric(12, 2)
  FROM invoices AS a LEFT JOIN customers b ON b.id = a.owner_id AND b.status IS NOT NULL
  WHERE a.owner_id BETWEEN 57 AND 767 AND a.id IN ('x', 'y', 'z') AND NOT a.region LIKE 'p%'
  GROUP BY 1, 2 HAVING count(*) > 4 ORDER BY n DESC, 1 LIMIT 17 OFFSET 912;
INSERT INTO invoices (amount, score, created_at) VALUES (227377, 'text 946748', now() + interval '29 days'), (443970, E'it''s\n', NULL)
  ON CONFLICT (id) DO UPDATE SET created_at = EXCLUDED.score RETURNING id;
UPDATE events SET amount = status + 12, amount = CASE WHEN name > 15 THEN 'big' ELSE 'small' END WHERE id = $1 AND name <> ALL (ARRAY[3, 7, 4]);
WITH recent AS (SELECT * FROM invoices WHERE status > current_date - 30), ranked AS (
  SELECT *, row_number() OVER (PARTITION BY created_at ORDER BY created_at DESC) AS rn FROM recent)
SELECT r.*, (SELECT max(id) FROM orders x WHERE x.id = r.id) FROM ranked r WHERE rn <= 0
UNION ALL SELECT * FROM orders WHERE EXISTS (SELECT 1 FROM orders WHERE amount = $2);
DELETE FROM orders USING events WHERE orders.owner_id = orders.id AND orders.created_at < now() - interval '1 year' RETURNING *;
SELECT a.created_at, b.amount AS "B Col", count(*) FILTER (WHERE a.created_at > 356) AS n, coalesce(sum(a.name), 0)::numeric(12, 2)
  FROM products AS a LEFT JOIN invoices b ON b.id = a.name AND b.owner_id IS NOT NULL
  WHERE a.region BETWEEN 21 AND 820 AND a.region IN ('x', 'y', 'z') AND NOT a.score LIKE 'p%'
  GROUP BY 1, 2 HAVING count(*) > 5 ORDER BY n DESC, 1 LIMIT 96 OFFSET 371;
INSERT INTO sessions (id, amount, score) VALUES (986765, 'text 790091', now() + interval '27 days'), (626483, E'it''s\n', NULL)
  ON CONFLICT (id) DO UPDATE SET created_at = EXCLUDED.id RETURNING id;
UPDATE events SET score = region + 90, name = CASE WHEN id > 685 THEN 'big' ELSE 'small' END WHERE id = $1 AND amount <> ALL (ARRAY[8, 5, 8]);
WITH recent AS (SELECT * FROM sessions WHERE score > current_date - 3), ranked AS (
  SELECT *, row_number() OVER (PARTITION BY score ORDER BY amount DESC) AS rn FROM recent)
SELECT r.*, (SELECT max(name) FROM sessions x WHERE x.id = r.id) FROM ranked r WHERE rn <= 9
UNION ALL SELECT * FROM events WHERE EXISTS (SELECT 1 FROM sessions WHERE created_at = $2);
DELETE FROM sessions USING orders WHERE orders.id = orders.id AND orders.name < now() - interval '1 year' RETURNING *;
SELECT a.region, b.status AS "B Col", count(*) FILTER (WHERE a.score > 912) AS n, coalesce(sum(a.created_at), 0)::numeric(12, 2)
  FROM orders AS a LEFT JOIN orders b ON b.id = a.status AND b.region IS NOT NULL
  WHERE a.status BETWEEN 57 AND 441 AND a.id IN ('x', 'y', 'z') AND NOT a.created_at LIKE 'p%'
  GROUP BY 1, 2 HAVING count(*) > 1 ORDER BY n DESC, 1 LIMIT 12 OFFSET 525;
INSERT INTO sessions (status, status, amount) VALUES (864320, 'text 981210', now() + interval '2 days'), (747280, E'it''s\n', NULL)
  ON CONFLICT (id) DO UPDATE SET region = EXCLUDED.name RETURNING id;
UPDATE invoices SET amount = name + 7, owner_id = CASE WHEN amount > 2 THEN 'big' ELSE 'small' END WHERE id = $1 AND owner_id <> ALL (ARRAY[8, 7, 1]);
WITH recent AS (SELECT * FROM products WHERE name > current_date - 74), ranked AS (
  SELECT *, row_number() OVER (PARTITION BY created_at ORDER BY score DESC) AS rn FROM recent)
SELECT r.*, (SELECT max(amount) FROM events x WHERE x.id = r.id) FROM ranked r WHERE rn <= 5
UNION ALL SELECT * FROM invoices WHERE EXISTS (SELECT 1 FROM invoices WHERE created_at = $2);
DELETE FROM customers USING sessions WHERE orders.status = orders.id AND orders.owner_id < now() - interval '1 year' RETURNING *;
SELECT a.status, b.score AS "B Col", count(*) FILTER (WHERE a.status > 207) AS n, coalesce(sum(a.amount), 0)::numeric(12, 2)
  FROM orders AS a LEFT JOIN products b ON b.id = a.score AND b.status IS NOT NULL
  WHERE a.created_at BETWEEN 25 AND 336 AND a.status IN ('x', 'y', 'z') AND NOT a.status LIKE 'p%'
  GROUP BY 1, 2 HAVING count(*) > 6 ORDER BY n DESC, 1 LIMIT 34 OFFSET 149;
INSERT INTO events (score, status, region) VALUES (5955, 'text 514155', now() + interval '23 days'), (173401, E'it''s\n', NULL)
  ON CONFLICT (id) DO UPDATE SET owner_id = EXCLUDED.name RETURNING id;
UPDATE orders SET id = status + 12, owner_id = CASE WHEN amount > 619 THEN 'big' ELSE 'small' END WHERE id = $1 AND region <> ALL (ARRAY[7, 6, 0]);
WITH recent AS (SELECT * FROM customers WHERE id > current_date - 28), ranked AS (
  SELECT *, row_number() OVER (PARTITION BY created_at ORDER BY region DESC) AS rn FROM recent)
SELECT r.*, (SELECT max(status) FROM invoices x WHERE x.id = r.id) FROM ranked r WHERE rn <= 3
UNION ALL SELECT * FROM events WHERE EXISTS (SELECT 1 FROM sessions WHERE name = $2);
DELETE FROM invoices USING customers WHERE orders.amount = orders.id AND orders.amount < now() - interval '1 year' RETURNING *;
SELECT a.created_at, b.owner_id AS "B Col", count(*) FILTER (WHERE a.region > 359) AS n, coalesce(sum(a.id), 0)::numeric(12, 2)
  FROM customers AS a LEFT JOIN sessions b ON b.id = a.name AND b.score IS NOT NULL
  WHERE a.id BETWEEN 97 AND 823 AND a.owner_id IN ('x', 'y', 'z') AND NOT a.amount LIKE 'p%'
  GROUP BY 1, 2 HAVING count(*) > 8 ORDER BY n DESC, 1 LIMIT 62 OFFSET 809;
INSERT INTO sessions (region, created_at, created_at) VALUES (568311, 'text 607285', now() + interval '25 days'), (298084, E'it''s\n', NULL)
  ON CONFLICT (id) DO UPDATE SET created_at = EXCLUDED.amount RETURNING id;
UPDATE customers SET status = score + 35, created_at = CASE WHEN name > 251 THEN 'big' ELSE 'small' END WHERE id = $1 AND amount <> ALL (ARRAY[9, 4, 1]);
WITH recent AS (SELECT * FROM sessions WHERE score > current_date - 18), ranked AS (
  SELECT *, row_number() OVER (PARTITION BY id ORDER BY owner_id DESC) AS rn FROM recent)
SELECT r.*, (SELECT max(region) FROM sessions x WHERE x.id = r.id) FROM ranked r WHERE rn <= 8
UNION ALL SELECT * FROM sessions WHERE EXISTS (SELECT 1 FROM sessions WHERE name = $2);
DELETE FROM orders USING events WHERE orders.region = orders.id AND orders.region < now() - interval '1 year' RETURNING *;
SELECT a.score, b.amount AS "B Col", count(*) FILTER (WHERE a.score > 104) AS n, coalesce(sum(a.region), 0)::numeric(12, 2)
  FROM products AS a LEFT JOIN orders b ON b.id = a.status AND b.name IS NOT NULL
  WHERE a.amount BETWEEN 6 AND 775 AND a.amount IN ('x', 'y', 'z') AND NOT a.name LIKE 'p%'
  GROUP BY 1, 2 HAVING count(*) > 4 ORDER BY n DESC, 1 LIMIT 88 OFFSET 838;
INSERT INTO invoices (created_at, id, region) VALUES (39039, 'text 246875', now() + interval '1 days'), (136111, E'it''s\n', NULL)
  ON CONFLICT (id) DO UPDATE SET name = EXCLUDED.name RETURNING id;
UPDATE products SET status = owner_id + 21, amount = CASE WHEN created_at > 558 THEN 'big' ELSE 'small' END WHERE id = $1 AND score <> ALL (ARRAY[0, 3, 6]);
WITH recent AS (SELECT * FROM events WHERE amount > current_date - 1), ranked AS (
  SELECT *, row_number() OVER (PARTITION BY created_at ORDER BY amount DESC) AS rn FROM recent)
SELECT r.*, (SELECT max(name) FROM orders x WHERE x.id = r.id) FROM ranked r WHERE rn <= 5
UNION ALL SELECT * FROM events WHERE EXISTS (SELECT 1 FROM events WHERE region = $2);
DELETE FROM events USING sessions WHERE orders.status = orders.id AND orders.score < now() - interval '1 year' RETURNING *;
SELECT a.owner_id, b.id AS "B Col", count(*) FILTER (WHERE a.score > 914) AS n, coalesce(sum(a.status), 0)::numeric(12, 2)
  FROM sessions AS a LEFT JOIN invoices b ON b.id = a.created_at AND b.region IS NOT NULL
  WHERE a.owner_id BETWEEN 76 AND 797 AND a.status IN ('x', 'y', 'z') AND NOT a.region LIKE 'p%'
  GROUP BY 1, 2 HAVING count(*) > 8 ORDER BY n DESC, 1 LIMIT 85 OFFSET 862;
INSERT INTO products (status, name, region) VALUES (256531, 'text 341663', now() + interval '17 days'), (333956, E'it''s\n', NULL)
  ON CONFLICT (id) DO UPDATE SET name = EXCLUDED.created_at RETURNING id;
UPDATE invoices SET name = score + 10, id = CASE WHEN owner_id > 781 THEN 'big' ELSE 'small' END WHERE id = $1 AND status <> ALL (ARRAY[6, 6, 9]);
WITH recent AS (SELECT * FROM products WHERE id > current_date - 80), ranked AS (
  SELECT *, row_number() OVER (PARTITION BY amount ORDER BY region DESC) AS rn FROM recent)
SELECT r.*, (SELECT max(score) FROM invoices x WHERE x.id = r.id) FROM ranked r WHERE rn <= 6
UNION ALL SELECT * FROM sessions WHERE EXISTS (SELECT 1 FROM orders WHERE id = $2);
DELETE FROM orders USING events WHERE orders.score = orders.id AND orders.score < now() - interval '1 year' RETURNING *;
SELECT a.status, b.id AS "B Col", count(*) FILTER (WHERE a.amount > 588) AS n, coalesce(sum(a.created_at), 0)::numeric(12, 2)
  FROM invoices AS a LEFT JOIN invoices b ON b.id = a.id AND b.status IS NOT NULL
  WHERE a.amount BETWEEN 82 AND 254 AND a.name IN ('x', 'y', 'z') AND NOT a.amount LIKE 'p%'
  GROUP BY 1, 2 HAVING count(*) > 4 ORDER BY n DESC, 1 LIMIT 70 OFFSET 349;
INSERT INTO orders (status, created_at, amount) VALUES (248128, 'text 422022', now() + interval '29 days'), (802129, E'it''s\n', NULL)
  ON CONFLICT (id) DO UPDATE SET score = EXCLUDED.created_at RETURNING id;
UPDATE events SET amount = id + 8, name = CASE WHEN id > 163 THEN 'big' ELSE 'small' END WHERE id = $1 AND name <> ALL (ARRAY[9, 4, 1]);
WITH recent AS (SELECT * FROM events WHERE created_at > current_date - 85), ranked AS (
  SELECT *, row_number() OVER (PARTITION BY owner_id ORDER BY owner_id DESC) AS rn FROM recent)
SELECT r.*, (SELECT max(status) FROM orders x WHERE x.id = r.id) FROM ranked r WHERE rn <= 4
UNION ALL SELECT * FROM orders WHERE EXISTS (SELECT 1 FROM sessions WHERE status = $2);
DELETE FROM invoices USING orders WHERE orders.amount = orders.id AND orders.amount < now() - interval '1 year' RETURNING *;
SELECT a.amount, b.name AS "B Col", count(*) FILTER (WHERE a.status > 82) AS n, coalesce(sum(a.region), 0)::numeric(12, 2)
  FROM events AS a LEFT JOIN sessions b ON b.id = a.region AND b.created_at IS NOT NULL
  WHERE a.region BETWEEN 74 AND 466 AND a.id IN ('x', 'y', 'z') AND NOT a.owner_id LIKE 'p%'
  GROUP BY 1, 2 HAVING count(*) > 6 ORDER BY n DESC, 1 LIMIT 65 OFFSET 508;
INSERT INTO orders (region, created_at, region) VALUES (846201, 'text 658263', now() + interval '13 days'), (467958, E'it''s\n', NULL)
  ON CONFLICT (id) DO UPDATE SET created_at = EXCLUDED.region RETURNING id;
UPDATE invoices SET score = owner_id + 23, region = CASE WHEN score > 673 THEN 'big' ELSE 'small' END WHERE id = $1 AND name <> ALL (ARRAY[0, 3, 3]);
WITH recent AS (SELECT * FROM products WHERE status > current_date - 72), ranked AS (
  SELECT *, row_number() OVER (PARTITION BY status ORDER BY owner_id DESC) AS rn FROM recent)
SELECT r.*, (SELECT max(created_at) FROM customers x WHERE x.id = r.id) FROM ranked r WHERE rn <= 1
UNION ALL SELECT * FROM events WHERE EXISTS (SELECT 1 FROM products WHERE name = $2);
DELETE FROM sessions USING products WHERE orders.owner_id = orders.id AND orders.status < now() - interval '1 year' RETURNING *;
SELECT a.owner_id, b.name AS "B Col", count(*) FILTER (WHERE a.created_at > 292) AS n, coalesce(sum(a.status), 0)::numeric(12, 2)
  FROM events AS a LEFT JOIN invoices b ON b.id = a.status AND b.amount IS NOT NULL
  WHERE a.region BETWEEN 34 AND 486 AND a.name IN ('x', 'y', 'z') AND NOT a.owner_id LIKE 'p%'
  GROUP BY 1, 2 HAVING count(*) > 6 ORDER BY n DESC, 1 LIMIT 29 OFFSET 475;
INSERT INTO sessions (score, created_at, owner_id) VALUES (127054, 'text 830821', now() + interval '27 days'), (788295, E'it''s\n', NULL)
  ON CONFLICT (id) DO UPDATE SET score = EXCLUDED.id RETURNING id;
UPDATE sessions SET id = region + 22, score = CASE WHEN status > 550 THEN 'big' ELSE 'small' END WHERE id = $1 AND amount <> ALL (ARRAY[7, 7, 4]);
WITH recent AS (SELECT * FROM customers WHERE amount > current_date - 9), ranked AS (
  SELECT *, row_number() OVER (PARTITION BY name ORDER BY status DESC) AS rn FROM recent)
SELECT r.*, (SELECT max(region) FROM orders x WHERE x.id = r.id) FROM ranked r WHERE rn <= 9
UNION ALL SELECT * FROM customers WHERE EXISTS (SELECT 1 FROM sessions WHERE created_at = $2);
DELETE FROM customers USING orders WHERE orders.region = orders.id AND orders.region < now() - interval '1 year' RETURNING *;
SELECT a.region, b.owner_id AS "B Col", count(*) FILTER (WHERE a.status > 742) AS n, coalesce(sum(a.status), 0)::numeric(12, 2)
  FROM products AS a LEFT JOIN sessions b ON b.id = a.amount AND b.region IS NOT NULL
  WHERE a.region BETWEEN 60 AND 632 AND a.id IN ('x', 'y', 'z') AND NOT a.amount LIKE 'p%'
  GROUP BY 1, 2 HAVING count(*) > 7 ORDER BY n DESC, 1 LIMIT 43 OFFSET 274;
INSERT INTO events (region, region, name) VALUES (227975, 'text 997206', now() + interval '23 days'), (501674, E'it''s\n', NULL)
  ON CONFLICT (id) DO UPDATE SET region = EXCLUDED.region RETURNING id;
UPDATE sessions SET id = owner_id + 71, created_at = CASE WHEN region > 818 THEN 'big' ELSE 'small' END WHERE id = $1 AND score <> ALL (ARRAY[8, 0, 8]);
WITH recent AS (SELECT * FROM invoices WHERE name > current_date - 11), ranked AS (
  SELECT *, row_number() OVER (PARTITION BY status ORDER BY amount DESC) AS rn FROM recent)
SELECT r.*, (SELECT max(amount) FROM customers x WHERE x.id = r.id) FROM ranked r WHERE rn <= 1
UNION ALL SELECT * FROM products WHERE EXISTS (SELECT 1 FROM customers WHERE region = $2);
DELETE FROM products USING invoices WHERE orders.amount = orders.id AND orders.name < now() - interval '1 year' RETURNING *;
SELECT a.region, b.score AS "B Col", count(*) FILTER (WHERE a.created_at > 429) AS n, coalesce(sum(a.score), 0)::numeric(12, 2)
  FROM sessions AS a LEFT JOIN products b ON b.id = a.name AND b.amount IS NOT NULL
  WHERE a.status BETWEEN 47 AND 293 AND a.id IN ('x', 'y', 'z') AND NOT a.amount LIKE 'p%'
  GROUP BY 1, 2 HAVING count(*) > 8 ORDER BY n DESC, 1 LIMIT 36 OFFSET 568;
INSERT INTO events (amount, created_at, score) VALUES (883345, 'text 817569', now() + interval '17 days'), (662159, E'it''s\n', NULL)
  ON CONFLICT (id) DO UPDATE SET owner_id = EXCLUDED.score RETURNING id;
UPDATE orders SET owner_id = amount + 8, created_at = CASE WHEN name > 583 THEN 'big' ELSE 'small' END WHERE id = $1 AND name <> ALL (ARRAY[7, 9, 0]);
WITH recent AS (SELECT * FROM orders WHERE name > current_date - 84), ranked AS (
  SELECT *, row_number() OVER (PARTITION BY id ORDER BY score DESC) AS rn FROM recent)
SELECT r.*, (SELECT max(created_at) FROM sessions x WHERE x.id = r.id) FROM ranked r WHERE rn <= 6
UNION ALL SELECT * FROM products WHERE EXISTS (SELECT 1 FROM events WHERE amount = $2);
DELETE FROM customers USING invoices WHERE orders.id = orders.id AND orders.score < now() - interval '1 year' RETURNING *;
SELECT a.score, b.region AS "B Col", count(*) FILTER (WHERE a.status > 52) AS n, coalesce(sum(a.name), 0)::numeric(12, 2)
  FROM invoices AS a LEFT JOIN customers b ON b.id = a.owner_id AND b.score IS NOT NULL
  WHERE a.region BETWEEN 1 AND 923 AND a.name IN ('x', 'y', 'z') AND NOT a.name LIKE 'p%'
  GROUP BY 1, 2 HAVING count(*) > 3 ORDER BY n DESC, 1 LIMIT 82 OFFSET 610;
INSERT INTO sessions (name, owner_id, created_at) VALUES (84229, 'text 809245', now() + interval '8 days'), (259895, E'it''s\n', NULL)
  ON CONFLICT (id) DO UPDATE SET score = EXCLUDED.amount RETURNING id;
UPDATE invoices SET status = created_at + 14, owner_id = CASE WHEN created_at > 693 THEN 'big' ELSE 'small' END WHERE id = $1 AND name <> ALL (ARRAY[3, 9, 0]);
WITH recent AS (SELECT * FROM sessions WHERE name > current_date - 5), ranked AS (
  SELECT *, row_number() OVER (PARTITION BY amount ORDER BY created_at DESC) AS rn FROM recent)
SELECT r.*, (SELECT max(owner_id) FROM invoices x WHERE x.id = r.id) FROM ranked r WHERE rn <= 0
UNION ALL SELECT * FROM customers WHERE EXISTS (SELECT 1 FROM orders WHERE score = $2);
DELETE FROM events USING orders WHERE orders.amount = orders.id AND orders.score < now() - interval '1 year' RETURNING *;
SELECT a.id, b.status AS "B Col", count(*) FILTER (WHERE a.status > 33) AS n, coalesce(sum(a.status), 0)::numeric(12, 2)
  FROM products AS a LEFT JOIN events b ON b.id = a.region AND b.amount IS NOT NULL
  WHERE a.owner_id BETWEEN 25 AND 139 AND a.id IN ('x', 'y', 'z') AND NOT a.name LIKE 'p%'
  GROUP BY 1, 2 HAVING count(*) > 9 ORDER BY n DESC, 1 LIMIT 67 OFFSET 950;
INSERT INTO products (region, score, id) VALUES (78471, 'text 685561', now() + interval '1 days'), (736599, E'it''s\n', NULL)
  ON CONFLICT (id) DO UPDATE SET id = EXCLUDED.status RETURNING id;
UPDATE orders SET score = score + 85, owner_id = CASE WHEN id > 160 THEN 'big' ELSE 'small' END WHERE id = $1 AND region <> ALL (ARRAY[2, 1, 3]);
WITH recent AS (SELECT * FROM events WHERE name > current_date - 45), ranked AS (
  SELECT *, row_number() OVER (PARTITION BY name ORDER BY created_at DESC) AS rn FROM recent)
SELECT r.*, (SELECT max(amount) FROM customers x WHERE x.id = r.id) FROM ranked r WHERE rn <= 9
UNION ALL SELECT * FROM customers WHERE EXISTS (SELECT 1 FROM invoices WHERE name = $2);
DELETE FROM orders USING sessions WHERE orders.owner_id = orders.id AND orders.created_at < now() - interval '1 year' RETURNING *;
SELECT a.created_at, b.amount AS "B Col", count(*) FILTER (WHERE a.owner_id > 244) AS n, coalesce(sum(a.name), 0)::numeric(12, 2)
  FROM customers AS a LEFT JOIN events b ON b.id = a.created_at AND b.amount IS NOT NULL
  WHERE a.id BETWEEN 22 AND 697 AND a.amount IN ('x', 'y', 'z') AND NOT a.owner_id LIKE 'p%'
  GROUP BY 1, 2 HAVING count(*) > 7 ORDER BY n DESC, 1 LIMIT 99 OFFSET 30;
INSERT INTO orders (amount, status, region) VALUES (910229, 'text 374036', now() + interval '15 days'), (116356, E'it''s\n', NULL)
  ON CONFLICT (id) DO UPDATE SET amount = EXCLUDED.id RETURNING id;
UPDATE events SET created_at = region + 51, amount = CASE WHEN status > 863 THEN 'big' ELSE 'small' END WHERE id = $1 AND created_at <> ALL (ARRAY[6, 5, 4]);
WITH recent AS (SELECT * FROM invoices WHERE id > current_date - 17), ranked AS (
  SELECT *, row_number() OVER (PARTITION BY region ORDER BY id DESC) AS rn FROM recent)
SELECT r.*, (SELECT max(created_at) FROM invoices x WHERE x.id = r.id) FROM ranked r WHERE rn <= 1
UNION ALL SELECT * FROM events WHERE EXISTS (SELECT 1 FROM invoices WHERE id = $2);
DELETE FROM sessions USING invoices WHERE orders.score = orders.id AND orders.region < now() - interval '1 year' RETURNING *;
SELECT a.score, b.score AS "B Col", count(*) FILTER (WHERE a.name > 84) AS n, coalesce(sum(a.name), 0)::numeric(12, 2)
  FROM customers AS a LEFT JOIN events b ON b.id = a.amount AND b.owner_id IS NOT NULL
  WHERE a.name BETWEEN 29 AND 315 AND a.created_at IN ('x', 'y', 'z') AND NOT a.id LIKE 'p%'
  GROUP BY 1, 2 HAVING count(*) > 8 ORDER BY n DESC, 1 LIMIT 76 OFFSET 475;
INSERT INTO customers (status, id, amount) VALUES (196816, 'text 47653', now() + interval '2 days'), (89916, E'it''s\n', NULL)
  ON CONFLICT (id) DO UPDATE SET name = EXCLUDED.amount RETURNING id;
UPDATE sessions SET owner_id = region + 4, status = CASE WHEN id > 398 THEN 'big' ELSE 'small' END WHERE id = $1 AND status <> ALL (ARRAY[9, 3, 2]);
WITH recent AS (SELECT * FROM products WHERE status > current_date - 32), ranked AS (
  SELECT *, row_number() OVER (PARTITION BY status ORDER BY status DESC) AS rn FROM recent)
SELECT r.*, (SELECT max(owner_id) FROM orders x WHERE x.id = r.id) FROM ranked r WHERE rn <= 4
UNION ALL SELECT * FROM customers WHERE EXISTS (SELECT 1 FROM customers WHERE owner_id = $2);
DELETE FROM customers USING products WHERE orders.id = orders.id AND orders.owner_id < now() - interval '1 year' RETURNING *;
SELECT a.status, b.owner_id AS "B Col", count(*) FILTER (WHERE a.amount > 914) AS n, coalesce(sum(a.created_at), 0)::numeric(12, 2)
  FROM events AS a LEFT JOIN invoices b ON b.id = a.score AND b.created_at IS NOT NULL
  WHERE a.created_at BETWEEN 44 AND 254 AND a.region IN ('x', 'y', 'z') AND NOT a.region LIKE 'p%'
  GROUP BY 1, 2 HAVING count(*) > 0 ORDER BY n DESC, 1 LIMIT 1 OFFSET 525;
INSERT INTO invoices (owner_id, amount, score) VALUES (583374, 'text 508887', now() + interval '24 days'), (736335, E'it''s\n', NULL)
  ON CONFLICT (id) DO UPDATE SET owner_id = EXCLUDED.score RETURNING id;
UPDATE customers SET name = status + 16, region = CASE WHEN created_at > 833 THEN 'big' ELSE 'small' END WHERE id = $1 AND status <> ALL (ARRAY[6, 5, 6]);
WITH recent AS (SELECT * FROM sessions WHERE id > current_date - 71), ranked AS (
  SELECT *, row_number() OVER (PARTITION BY status ORDER BY status DESC) AS rn FROM recent)
SELECT r.*, (SELECT max(status) FROM products x WHERE x.id = r.id) FROM ranked r WHERE rn <= 9
UNION ALL SELECT * FROM customers WHERE EXISTS (SELECT 1 FROM products WHERE owner_id = $2);
DELETE FROM invoices USING events WHERE orders.score = orders.id AND orders.region < now() - interval '1 year' RETURNING *;
SELECT a.id, b.region AS "B Col", count(*) FILTER (WHERE a.status > 299) AS n, coalesce(sum(a.name), 0)::numeric(12, 2)
  FROM sessions AS a LEFT JOIN customers b ON b.id = a.score AND b.score IS NOT NULL
  WHERE a.score BETWEEN 28 AND 132 AND a.created_at IN ('x', 'y', 'z') AND NOT a.owner_id LIKE 'p%'
  GROUP BY 1, 2 HAVING count(*) > 6 ORDER BY n DESC, 1 LIMIT 74 OFFSET 723;
INSERT INTO sessions (status, name, owner_id) VALUES (682593, 'text 985973', now() + interval '4 days'), (747943, E'it''s\n', NULL)
  ON CONFLICT (id) DO UPDATE SET amount = EXCLUDED.name RETURNING id;
UPDATE products SET id = amount + 16, amount = CASE WHEN score > 243 THEN 'big' ELSE 'small' END WHERE id = $1 AND score <> ALL (ARRAY[6, 5, 5]);
WITH recent AS (SELECT * FROM orders WHERE status > current_date - 25), ranked AS (
  SELECT *, row_number() OVER (PARTITION BY score ORDER BY id DESC) AS rn FROM recent)
SELECT r.*, (SELECT max(region) FROM customers x WHERE x.id = r.id) FROM ranked r WHERE rn <= 6
UNION ALL SELECT * FROM products WHERE EXISTS (SELECT 1 FROM invoices WHERE created_at = $2);
DELETE FROM sessions USING invoices WHERE orders.score = orders.id AND orders.id < now() - interval '1 year' RETURNING *;
SELECT a.owner_id, b.amount AS "B Col", count(*) FILTER (WHERE a.owner_id > 607) AS n, coalesce(sum(a.name), 0)::numeric(12, 2)
  FROM products AS a LEFT JOIN events b ON b.id = a.score AND b.amount IS NOT NULL
  WHERE a.name BETWEEN 99 AND 223 AND a.amount IN ('x', 'y', 'z') AND NOT a.id LIKE 'p%'
  GROUP BY 1, 2 HAVING count(*) > 3 ORDER BY n DESC, 1 LIMIT 95 OFFSET 925;
INSERT INTO sessions (status, created_at, status) VALUES (839578, 'text 448688', now() + interval '22 days'), (45135, E'it''s\n', NULL)
  ON CONFLICT (id) DO UPDATE SET name = EXCLUDED.region RETURNING id;
UPDATE sessions SET amount = id + 32, score = CASE WHEN score > 715 THEN 'big' ELSE 'small' END WHERE id = $1 AND id <> ALL (ARRAY[0, 6, 9]);
WITH recent AS (SELECT * FROM events WHERE owner_id > current_date - 77), ranked AS (
  SELECT *, row_number() OVER (PARTITION BY amount ORDER BY owner_id DESC) AS rn FROM recent)
SELECT r.*, (SELECT max(amount) FROM events x WHERE x.id = r.id) FROM ranked r WHERE rn <= 1
UNION ALL SELECT * FROM events WHERE EXISTS (SELECT 1 FROM orders WHERE status = $2);
DELETE FROM customers USING orders WHERE orders.owner_id = orders.id AND orders.status < now() - interval '1 year' RETURNING *;
SELECT a.owner_id, b.amount AS "B Col", count(*) FILTER (WHERE a.created_at > 43) AS n, coalesce(sum(a.name), 0)::numeric(12, 2)
  FROM products AS a LEFT JOIN orders b ON b.id = a.region AND b.created_at IS NOT NULL
  WHERE a.id BETWEEN 59 AND 144 AND a.amount IN ('x', 'y', 'z') AND NOT a.owner_id LIKE 'p%'
  GROUP BY 1, 2 HAVING count(*) > 4 ORDER BY n DESC, 1 LIMIT 52 OFFSET 657;
INSERT INTO events (amount, status, name) VALUES (492927, 'text 656966', now() + interval '13 days'), (333724, E'it''s\n', NULL)
  ON CONFLICT (id) DO UPDATE SET owner_id = EXCLUDED.id RETURNING id;
UPDATE invoices SET amount = region + 18, amount = CASE WHEN owner_id > 30 THEN 'big' ELSE 'small' END WHERE id = $1 AND name <> ALL (ARRAY[8, 8, 4]);
WITH recent AS (SELECT * FROM events WHERE created_at > current_date - 64), ranked AS (
  SELECT *, row_number() OVER (PARTITION BY region ORDER BY id DESC) AS rn FROM recent)
SELECT r.*, (SELECT max(amount) FROM invoices x WHERE x.id = r.id) FROM ranked r WHERE rn <= 1
UNION ALL SELECT * FROM sessions WHERE EXISTS (SELECT 1 FROM customers WHERE region = $2);
DELETE FROM events USING events WHERE orders.amount = orders.id AND orders.created_at < now() - interval '1 year' RETURNING *;
SELECT a.score, b.owner_id AS "B Col", count(*) FILTER (WHERE a.score > 650) AS n, coalesce(sum(a.amount), 0)::numeric(12, 2)
  FROM orders AS a LEFT JOIN events b ON b.id = a.name AND b.amount IS NOT NULL
  WHERE a.created_at BETWEEN 0 AND 985 AND a.owner_id IN ('x', 'y', 'z') AND NOT a.created_at LIKE 'p%'
  GROUP BY 1, 2 HAVING count(*) > 3 ORDER BY n DESC, 1 LIMIT 4 OFFSET 14;
INSERT INTO sessions (id, region, status) VALUES (443497, 'text 80126', now() + interval '27 days'), (810179, E'it''s\n', NULL)
  ON CONFLICT (id) DO UPDATE SET amount = EXCLUDED.region RETURNING id;
UPDATE customers SET amount = score + 12, region = CASE WHEN name > 429 THEN 'big' ELSE 'small' END WHERE id = $1 AND amount <> ALL (ARRAY[3, 1, 6]);
WITH recent AS (SELECT * FROM customers WHERE amount > current_date - 85), ranked AS (
  SELECT *, row_number() OVER (PARTITION BY name ORDER BY amount DESC) AS rn FROM recent)
SELECT r.*, (SELECT max(owner_id) FROM products x WHERE x.id = r.id) FROM ranked r WHERE rn <= 8
UNION ALL SELECT * FROM customers WHERE EXISTS (SELECT 1 FROM orders WHERE owner_id = $2);
DELETE FROM invoices USING orders WHERE orders.score = orders.id AND orders.owner_id < now() - interval '1 year' RETURNING *;
SELECT a.amount, b.id AS "B Col", count(*) FILTER (WHERE a.status > 323) AS n, coalesce(sum(a.status), 0)::numeric(12, 2)
  FROM events AS a LEFT JOIN products b ON b.id = a.amount AND b.name IS NOT NULL
  WHERE a.amount BETWEEN 31 AND 498 AND a.created_at IN ('x', 'y', 'z') AND NOT a.owner_id LIKE 'p%'
  GROUP BY 1, 2 HAVING count(*) > 7 ORDER BY n DESC, 1 LIMIT 65 OFFSET 107;
INSERT INTO orders (owner_id, region, owner_id) VALUES (517194, 'text 272169', now() + interval '18 days'), (628030, E'it''s\n', NULL)
  ON CONFLICT (id) DO UPDATE SET status = EXCLUDED.created_at RETURNING id;
UPDATE orders SET score = region + 18, owner_id = CASE WHEN owner_id > 944 THEN 'big' ELSE 'small' END WHERE id = $1 AND score <> ALL (ARRAY[2, 9, 1]);
WITH recent AS (SELECT * FROM orders WHERE amount > current_date - 82), ranked AS (
  SELECT *, row_number() OVER (PARTITION BY amount ORDER BY amount DESC) AS rn FROM recent)
SELECT r.*, (SELECT max(amount) FROM orders x WHERE x.id = r.id) FROM ranked r WHERE rn <= 8
UNION ALL SELECT * FROM customers WHERE EXISTS (SELECT 1 FROM products WHERE amount = $2);
DELETE FROM orders USING customers WHERE orders.id = orders.id AND orders.amount < now() - interval '1 year' RETURNING *;
SELECT a.owner_id, b.status AS "B Col", count(*) FILTER (WHERE a.amount > 449) AS n, coalesce(sum(a.owner_id), 0)::numeric(12, 2)
  FROM invoices AS a LEFT JOIN orders b ON b.id = a.created_at AND b.name IS NOT NULL
  WHERE a.status BETWEEN 24 AND 245 AND a.owner_id IN ('x', 'y', 'z') AND NOT a.owner_id LIKE 'p%'
  GROUP BY 1, 2 HAVING count(*) > 5 ORDER BY n DESC, 1 LIMIT 53 OFFSET 382;
INSERT INTO products (id, owner_id, name) VALUES (419714, 'text 572186', now() + interval '29 days'), (910630, E'it''s\n', NULL)
  ON CONFLICT (id) DO UPDATE SET created_at = EXCLUDED.owner_id RETURNING id;
UPDATE orders SET score = score + 51, status = CASE WHEN status > 686 THEN 'big' ELSE 'small' END WHERE id = $1 AND name <> ALL (ARRAY[8, 9, 5]);
WITH recent AS (SELECT * FROM invoices WHERE created_at > current_date - 20), ranked AS (
  SELECT *, row_number() OVER (PARTITION BY created_at ORDER BY score DESC) AS rn FROM recent)
SELECT r.*, (SELECT max(score) FROM events x WHERE x.id = r.id) FROM ranked r WHERE rn <= 8
UNION ALL SELECT * FROM orders WHERE EXISTS (SELECT 1 FROM invoices WHERE name = $2);
DELETE FROM orders USING invoices WHERE orders.amount = orders.id AND orders.created_at < now() - interval '1 year' RETURNING *;
SELECT a.owner_id, b.id AS "B Col", count(*) FILTER (WHERE a.id > 743) AS n, coalesce(sum(a.owner_id), 0)::numeric(12, 2)
  FROM invoices AS a LEFT JOIN invoices b ON b.id = a.name AND b.name IS NOT NULL
  WHERE a.status BETWEEN 93 AND 787 AND a.owner_id IN ('x', 'y', 'z') AND NOT a.name LIKE 'p%'
  GROUP BY 1, 2 HAVING count(*) > 1 ORDER BY n DESC, 1 LIMIT 21 OFFSET 80;
INSERT INTO events (region, id, amount) VALUES (718069, 'text 772607', now() + interval '27 days'), (974043, E'it''s\n', NULL)
  ON CONFLICT (id) DO UPDATE SET score = EXCLUDED.amount RETURNING id;
UPDATE sessions SET owner_id = created_at + 50, score = CASE WHEN amount > 829 THEN 'big' ELSE 'small' END WHERE id = $1 AND owner_id <> ALL (ARRAY[5, 6, 9]);
WITH recent AS (SELECT * FROM sessions WHERE name > current_date - 16), ranked AS (
  SELECT *, row_number() OVER (PARTITION BY owner_id ORDER BY status DESC) AS rn FROM recent)
SELECT r.*, (SELECT max(id) FROM products x WHERE x.id = r.id) FROM ranked r WHERE rn <= 7
UNION ALL SELECT * FROM sessions WHERE EXISTS (SELECT 1 FROM sessions WHERE status = $2);
DELETE FROM invoices USING sessions WHERE orders.amount = orders.id AND orders.id < now() - interval '1 year' RETURNING *;
SELECT a.owner_id, b.status AS "B Col", count(*) FILTER (WHERE a.status > 816) AS n, coalesce(sum(a.amount), 0)::numeric(12, 2)
  FROM sessions AS a LEFT JOIN sessions b ON b.id = a.region AND b.region IS NOT NULL
  WHERE a.amount BETWEEN 1 AND 953 AND a.id IN ('x', 'y', 'z') AND NOT a.amount LIKE 'p%'
  GROUP BY 1, 2 HAVING count(*) > 1 ORDER BY n DESC, 1 LIMIT 23 OFFSET 993;
INSERT INTO events (created_at, name, name) VALUES (826828, 'text 475185', now() + interval '23 days'), (449107, E'it''s\n', NULL)
  ON CONFLICT (id) DO UPDATE SET created_at = EXCLUDED.id RETURNING id;
UPDATE customers SET name = created_at + 40, name = CASE WHEN owner_id > 924 THEN 'big' ELSE 'small' END WHERE id = $1 AND name <> ALL (ARRAY[1, 1, 8]);
WITH recent AS (SELECT * FROM sessions WHERE amount > current_date - 34), ranked AS (
  SELECT *, row_number() OVER (PARTITION BY name ORDER BY id DESC) AS rn FROM recent)
SELECT r.*, (SELECT max(owner_id) FROM products x WHERE x.id = r.id) FROM ranked r WHERE rn <= 8
UNION ALL SELECT * FROM orders WHERE EXISTS (SELECT 1 FROM events WHERE id = $2);
DELETE FROM orders USING orders WHERE orders.status = orders.id AND orders.owner_id < now() - interval '1 year' RETURNING *;
SELECT a.region, b.amount AS "B Col", count(*) FILTER (WHERE a.created_at > 247) AS n, coalesce(sum(a.owner_id), 0)::numeric(12, 2)
  FROM orders AS a LEFT JOIN sessions b ON b.id = a.amount AND b.score IS NOT NULL
  WHERE a.created_at BETWEEN 74 AND 979 AND a.status IN ('x', 'y', 'z') AND NOT a.region LIKE 'p%'
  GROUP BY 1, 2 HAVING count(*) > 7 ORDER BY n DESC, 1 LIMIT 57 OFFSET 8;
INSERT INTO events (name, status, status) VALUES (782586, 'text 287992', now() + interval '12 days'), (810955, E'it''s\n', NULL)
  ON CONFLICT (id) DO UPDATE SET status = EXCLUDED.id RETURNING id;
UPDATE invoices SET created_at = score + 82, amount = CASE WHEN score > 249 THEN 'big' ELSE 'small' END WHERE id = $1 AND created_at <> ALL (ARRAY[8, 0, 2]);
WITH recent AS (SELECT * FROM products WHERE created_at > current_date - 14), ranked AS (
  SELECT *, row_number() OVER (PARTITION BY status ORDER BY amount DESC) AS rn FROM recent)
SELECT r.*, (SELECT max(name) FROM invoices x WHERE x.id = r.id) FROM ranked r WHERE rn <= 5
UNION ALL SELECT * FROM orders WHERE EXISTS (SELECT 1 FROM products WHERE name = $2);
DELETE FROM sessions USING products WHERE orders.status = orders.id AND orders.created_at < now() - interval '1 year' RETURNING *;
SELECT a.created_at, b.status AS "B Col", count(*) FILTER (WHERE a.id > 851) AS n, coalesce(sum(a.created_at), 0)::numeric(12, 2)
  FROM orders AS a LEFT JOIN sessions b ON b.id = a.id AND b.created_at IS NOT NULL
  WHERE a.id BETWEEN 68 AND 414 AND a.id IN ('x', 'y', 'z') AND NOT a.id LIKE 'p%'
  GROUP BY 1, 2 HAVING count(*) > 5 ORDER BY n DESC, 1 LIMIT 26 OFFSET 543;
INSERT INTO invoices (score, created_at, owner_id) VALUES (330175, 'text 995837', now() + interval '8 days'), (350434, E'it''s\n', NULL)
  ON CONFLICT (id) DO UPDATE SET owner_id = EXCLUDED.created_at RETURNING id;
UPDATE products SET id = region + 83, score = CASE WHEN score > 920 THEN 'big' ELSE 'small' END WHERE id = $1 AND score <> ALL (ARRAY[7, 3, 5]);
WITH recent AS (SELECT * FROM orders WHERE owner_id > current_date - 72), ranked AS (
  SELECT *, row_number() OVER (PARTITION BY created_at ORDER BY owner_id DESC) AS rn FROM recent)
SELECT r.*, (SELECT max(name) FROM customers x WHERE x.id = r.id) FROM ranked r WHERE rn <= 9
UNION ALL SELECT * FROM invoices WHERE EXISTS (SELECT 1 FROM sessions WHERE created_at = $2);
DELETE FROM orders USING customers WHERE orders.id = orders.id AND orders.score < now() - interval '1 year' RETURNING *;
SELECT a.created_at, b.amount AS "B Col", count(*) FILTER (WHERE a.score > 420) AS n, coalesce(sum(a.id), 0)::numeric(12, 2)
  FROM invoices AS a LEFT JOIN customers b ON b.id = a.score AND b.id IS NOT NULL
  WHERE a.name BETWEEN 63 AND 348 AND a.owner_id IN ('x', 'y', 'z') AND NOT a.status LIKE 'p%'
  GROUP BY 1, 2 HAVING count(*) > 1 ORDER BY n DESC, 1 LIMIT 84 OFFSET 694;
INSERT INTO customers (id, amount, region) VALUES (561481, 'text 47905', now() + interval '27 days'), (200099, E'it''s\n', NULL)
  ON CONFLICT (id) DO UPDATE SET status = EXCLUDED.created_at RETURNING id;
UPDATE customers SET owner_id = created_at + 98, id = CASE WHEN region > 119 THEN 'big' ELSE 'small' END WHERE id = $1 AND amount <> ALL (ARRAY[1, 0, 3]);
WITH recent AS (SELECT * FROM products WHERE owner_id > current_date - 66), ranked AS (
  SELECT *, row_number() OVER (PARTITION BY status ORDER BY name DESC) AS rn FROM recent)
SELECT r.*, (SELECT max(created_at) FROM customers x WHERE x.id = r.id) FROM ranked r WHERE rn <= 3
UNION ALL SELECT * FROM customers WHERE EXISTS (SELECT 1 FROM products WHERE score = $2);
DELETE FROM sessions USING events WHERE orders.region = orders.id AND orders.amount < now() - interval '1 year' RETURNING *;
SELECT a.status, b.score AS "B Col", count(*) FILTER (WHERE a.created_at > 756) AS n, coalesce(sum(a.score), 0)::numeric(12, 2)
  FROM products AS a LEFT JOIN customers b ON b.id = a.owner_id AND b.name IS NOT NULL
  WHERE a.name BETWEEN 73 AND 1077 AND a.id IN ('x', 'y', 'z') AND NOT a.score LIKE 'p%'
  GROUP BY 1, 2 HAVING count(*) > 0 ORDER BY n DESC, 1 LIMIT 58 OFFSET 195;
INSERT INTO products (id, owner_id, status) VALUES (188759, 'text 165055', now() + interval '23 days'), (304773, E'it''s\n', NULL)
  ON CONFLICT (id) DO UPDATE SET created_at = EXCLUDED.status RETURNING id;
UPDATE products SET id = score + 20, score = CASE WHEN created_at > 547 THEN 'big' ELSE 'small' END WHERE id = $1 AND owner_id <> ALL (ARRAY[3, 4, 3]);
WITH recent AS (SELECT * FROM customers WHERE created_at > current_date - 18), ranked AS (
  SELECT *, row_number() OVER (PARTITION BY status ORDER BY status DESC) AS rn FROM recent)
SELECT r.*, (SELECT max(amount) FROM customers x WHERE x.id = r.id) FROM ranked r WHERE rn <= 7
UNION ALL SELECT * FROM products WHERE EXISTS (SELECT 1 FROM events WHERE owner_id = $2);
DELETE FROM invoices USING invoices WHERE orders.amount = orders.id AND orders.region < now() - interval '1 year' RETURNING *;
SELECT a.score, b.status AS "B Col", count(*) FILTER (WHERE a.name > 92) AS n, coalesce(sum(a.id), 0)::numeric(12, 2)
  FROM orders AS a LEFT JOIN events b ON b.id = a.owner_id AND b.owner_id IS NOT NULL
  WHERE a.score BETWEEN 25 AND 218 AND a.amount IN ('x', 'y', 'z') AND NOT a.score LIKE 'p%'
  GROUP BY 1, 2 HAVING count(*) > 0 ORDER BY n DESC, 1 LIMIT 13 OFFSET 178;
INSERT INTO events (owner_id, id, owner_id) VALUES (949691, 'text 904143', now() + interval '0 days'), (406931, E'it''s\n', NULL)
  ON CONFLICT (id) DO UPDATE SET id = EXCLUDED.status RETURNING id;
UPDATE customers SET id = created_at + 8, status = CASE WHEN score > 328 THEN 'big' ELSE 'small' END WHERE id = $1 AND amount <> ALL (ARRAY[0, 7, 0]);
WITH recent AS (SELECT * FROM sessions WHERE id > current_date - 68), ranked AS (
  SELECT *, row_number() OVER (PARTITION BY id ORDER BY created_at DESC) AS rn FROM recent)
SELECT r.*, (SELECT max(created_at) FROM events x WHERE x.id = r.id) FROM ranked r WHERE rn <= 5
UNION ALL SELECT * FROM customers WHERE EXISTS (SELECT 1 FROM invoices WHERE status = $2);
DELETE FROM products USING invoices WHERE orders.status = orders.id AND orders.region < now() - interval '1 year' RETURNING *;
SELECT a.amount, b.created_at AS "B Col", count(*) FILTER (WHERE a.created_at > 50) AS n, coalesce(sum(a.owner_id), 0)::numeric(12, 2)
  FROM products AS a LEFT JOIN events b ON b.id = a.id AND b.created_at IS NOT NULL
  WHERE a.score BETWEEN 26 AND 372 AND a.name IN ('x', 'y', 'z') AND NOT a.owner_id LIKE 'p%'
  GROUP BY 1, 2 HAVING count(*) > 1 ORDER BY n DESC, 1 LIMIT 7 OFFSET 179;
INSERT INTO products (owner_id, amount, region) VALUES (951199, 'text 105051', now() + interval '28 days'), (49267, E'it''s\n', NULL)
  ON CONFLICT (id) DO UPDATE SET status = EXCLUDED.created_at RETURNING id;
UPDATE customers SET owner_id = amount + 43, status = CASE WHEN score > 205 THEN 'big' ELSE 'small' END WHERE id = $1 AND name <> ALL (ARRAY[6, 2, 8]);
WITH recent AS (SELECT * FROM customers WHERE id > current_date - 22), ranked AS (
  SELECT *, row_number() OVER (PARTITION BY amount ORDER BY score DESC) AS rn FROM recent)
SELECT r.*, (SELECT max(region) FROM products x WHERE x.id = r.id) FROM ranked r WHERE rn <= 4
UNION ALL SELECT * FROM customers WHERE EXISTS (SELECT 1 FROM sessions WHERE status = $2);
DELETE FROM sessions USING products WHERE orders.created_at = orders.id AND orders.owner_id < now() - interval '1 year' RETURNING *;
SELECT a.owner_id, b.name AS "B Col", count(*) FILTER (WHERE a.name > 686) AS n, coalesce(sum(a.score), 0)::numeric(12, 2)
  FROM invoices AS a LEFT JOIN orders b ON b.id = a.status AND b.created_at IS NOT NULL
  WHERE a.score BETWEEN 42 AND 169 AND a.owner_id IN ('x', 'y', 'z') AND NOT a.amount LIKE 'p%'
  GROUP BY 1, 2 HAVING count(*) > 4 ORDER BY n DESC, 1 LIMIT 89 OFFSET 944;
INSERT INTO events (created_at, id, id) VALUES (726335, 'text 606701', now() + interval '1 days'), (762393, E'it''s\n', NULL)
  ON CONFLICT (id) DO UPDATE SET name = EXCLUDED.name RETURNING id;
UPDATE products SET id = region + 54, status = CASE WHEN region > 771 THEN 'big' ELSE 'small' END WHERE id = $1 AND id <> ALL (ARRAY[0, 0, 9]);
WITH recent AS (SELECT * FROM invoices WHERE score > current_date - 49), ranked AS (
  SELECT *, row_number() OVER (PARTITION BY amount ORDER BY status DESC) AS rn FROM recent)
SELECT r.*, (SELECT max(id) FROM orders x WHERE x.id = r.id) FROM ranked r WHERE rn <= 9
UNION ALL SELECT * FROM orders WHERE EXISTS (SELECT 1 FROM invoices WHERE amount = $2);
DELETE FROM products USING events WHERE orders.id = orders.id AND orders.status < now() - interval '1 year' RETURNING *;
SELECT a.owner_id, b.owner_id AS "B Col", count(*) FILTER (WHERE a.name > 32) AS n, coalesce(sum(a.owner_id), 0)::numeric(12, 2)
  FROM products AS a LEFT JOIN sessions b ON b.id = a.status AND b.status IS NOT NULL
  WHERE a.id BETWEEN 45 AND 982 AND a.status IN ('x', 'y', 'z') AND NOT a.created_at LIKE 'p%'
  GROUP BY 1, 2 HAVING count(*) > 6 ORDER BY n DESC, 1 LIMIT 55 OFFSET 448;
INSERT INTO sessions (name, created_at, amount) VALUES (350327, 'text 287777', now() + interval '16 days'), (187500, E'it''s\n', NULL)
  ON CONFLICT (id) DO UPDATE SET created_at = EXCLUDED.owner_id RETURNING id;
UPDATE orders SET amount = status + 2, owner_id = CASE WHEN name > 244 THEN 'big' ELSE 'small' END WHERE id = $1 AND region <> ALL (ARRAY[2, 6, 2]);
WITH recent AS (SELECT * FROM sessions WHERE score > current_date - 55), ranked AS (
  SELECT *, row_number() OVER (PARTITION BY name ORDER BY owner_id DESC) AS rn FROM recent)
SELECT r.*, (SELECT max(amount) FROM orders x WHERE x.id = r.id) FROM ranked r WHERE rn <= 0
UNION ALL SELECT * FROM orders WHERE EXISTS (SELECT 1 FROM events WHERE region = $2);
DELETE FROM products USING events WHERE orders.score = orders.id AND orders.amount < now() - interval '1 year' RETURNING *;
SELECT a.id, b.id AS "B Col", count(*) FILTER (WHERE a.name > 741) AS n, coalesce(sum(a.status), 0)::numeric(12, 2)
  FROM events AS a LEFT JOIN orders b ON b.id = a.owner_id AND b.amount IS NOT NULL
  WHERE a.created_at BETWEEN 54 AND 364 AND a.owner_id IN ('x', 'y', 'z') AND NOT a.id LIKE 'p%'
  GROUP BY 1, 2 HAVING count(*) > 9 ORDER BY n DESC, 1 LIMIT 83 OFFSET 546;
INSERT INTO products (id, score, id) VALUES (168489, 'text 629766', now() + interval '11 days'), (41376, E'it''s\n', NULL)
  ON CONFLICT (id) DO UPDATE SET amount = EXCLUDED.created_at RETURNING id;
UPDATE orders SET amount = amount + 15, owner_id = CASE WHEN score > 420 THEN 'big' ELSE 'small' END WHERE id = $1 AND amount <> ALL (ARRAY[6, 9, 2]);
WITH recent AS (SELECT * FROM products WHERE created_at > current_date - 68), ranked AS (
  SELECT *, row_number() OVER (PARTITION BY score ORDER BY amount DESC) AS rn FROM recent)
SELECT r.*, (SELECT max(score) FROM events x WHERE x.id = r.id) FROM ranked r WHERE rn <= 2
UNION ALL SELECT * FROM orders WHERE EXISTS (SELECT 1 FROM events WHERE name = $2);
DELETE FROM events USING sessions WHERE orders.status = orders.id AND orders.id < now() - interval '1 year' RETURNING *;
SELECT a.status, b.status AS "B Col", count(*) FILTER (WHERE a.amount > 254) AS n, coalesce(sum(a.owner_id), 0)::numeric(12, 2)
  FROM products AS a LEFT JOIN customers b ON b.id = a.amount AND b.region IS NOT NULL
  WHERE a.id BETWEEN 66 AND 1074 AND a.amount IN ('x', 'y', 'z') AND NOT a.id LIKE 'p%'
  GROUP BY 1, 2 HAVING count(*) > 3 ORDER BY n DESC, 1 LIMIT 6 OFFSET 480;
INSERT INTO sessions (status, created_at, status) VALUES (686904, 'text 544806', now() + interval '13 days'), (41949, E'it''s\n', NULL)
  ON CONFLICT (id) DO UPDATE SET region = EXCLUDED.id RETURNING id;
UPDATE invoices SET owner_id = name + 36, amount = CASE WHEN score > 907 THEN 'big' ELSE 'small' END WHERE id = $1 AND owner_id <> ALL (ARRAY[5, 8, 6]);
WITH recent AS (SELECT * FROM sessions WHERE name > current_date - 87), ranked AS (
  SELECT *, row_number() OVER (PARTITION BY name ORDER BY id DESC) AS rn FROM recent)
SELECT r.*, (SELECT max(amount) FROM orders x WHERE x.id = r.id) FROM ranked r WHERE rn <= 4
UNION ALL SELECT * FROM sessions WHERE EXISTS (SELECT 1 FROM customers WHERE created_at = $2);
DELETE FROM sessions USING events WHERE orders.created_at = orders.id AND orders.region < now() - interval '1 year' RETURNING *;
SELECT a.created_at, b.region AS "B Col", count(*) FILTER (WHERE a.id > 543) AS n, coalesce(sum(a.id), 0)::numeric(12, 2)
  FROM events AS a LEFT JOIN customers b ON b.id = a.created_at AND b.name IS NOT NULL
  WHERE a.status BETWEEN 77 AND 887 AND a.owner_id IN ('x', 'y', 'z') AND NOT a.id LIKE 'p%'
  GROUP BY 1, 2 HAVING count(*) > 7 ORDER BY n DESC, 1 LIMIT 39 OFFSET 8;
INSERT INTO sessions (score, name, status) VALUES (140900, 'text 345046', now() + interval '17 days'), (104433, E'it''s\n', NULL)
  ON CONFLICT (id) DO UPDATE SET status = EXCLUDED.status RETURNING id;
UPDATE customers SET region = score + 56, status = CASE WHEN amount > 865 THEN 'big' ELSE 'small' END WHERE id = $1 AND name <> ALL (ARRAY[1, 4, 0]);
WITH recent AS (SELECT * FROM customers WHERE region > current_date - 54), ranked AS (
  SELECT *, row_number() OVER (PARTITION BY status ORDER BY region DESC) AS rn FROM recent)
SELECT r.*, (SELECT max(id) FROM sessions x WHERE x.id = r.id) FROM ranked r WHERE rn <= 3
UNION ALL SELECT * FROM orders WHERE EXISTS (SELECT 1 FROM orders WHERE amount = $2);
DELETE FROM customers USING products WHERE orders.status = orders.id AND orders.region < now() - interval '1 year' RETURNING *;
SELECT a.score, b.region AS "B Col", count(*) FILTER (WHERE a.id > 90) AS n, coalesce(sum(a.created_at), 0)::numeric(12, 2)
  FROM products AS a LEFT JOIN events b ON b.id = a.amount AND b.amount IS NOT NULL
  WHERE a.created_at BETWEEN 12 AND 1078 AND a.status IN ('x', 'y', 'z') AND NOT a.status LIKE 'p%'
  GROUP BY 1, 2 HAVING count(*) > 0 ORDER BY n DESC, 1 LIMIT 74 OFFSET 325;
INSERT INTO customers (amount, score, owner_id) VALUES (290661, 'text 24506', now() + interval '2 days'), (29511, E'it''s\n', NULL)
  ON CONFLICT (id) DO UPDATE SET created_at = EXCLUDED.created_at RETURNING id;
UPDATE customers SET amount = amount + 33, name = CASE WHEN owner_id > 576 THEN 'big' ELSE 'small' END WHERE id = $1 AND amount <> ALL (ARRAY[7, 5, 2]);
WITH recent AS (SELECT * FROM sessions WHERE owner_id > current_date - 60), ranked AS (
  SELECT *, row_number() OVER (PARTITION BY amount ORDER BY status DESC) AS rn FROM recent)
SELECT r.*, (SELECT max(id) FROM sessions x WHERE x.id = r.id) FROM ranked r WHERE rn <= 8
UNION ALL SELECT * FROM orders WHERE EXISTS (SELECT 1 FROM sessions WHERE score = $2);
DELETE FROM events USING events WHERE orders.amount = orders.id AND orders.name < now() - interval '1 year' RETURNING *;
SELECT a.amount, b.region AS "B Col", count(*) FILTER (WHERE a.score > 151) AS n, coalesce(sum(a.owner_id), 0)::numeric(12, 2)
  FROM events AS a LEFT JOIN sessions b ON b.id = a.created_at AND b.owner_id IS NOT NULL
  WHERE a.status BETWEEN 49 AND 465 AND a.score IN ('x', 'y', 'z') AND NOT a.amount LIKE 'p%'
  GROUP BY 1, 2 HAVING count(*) > 6 ORDER BY n DESC, 1 LIMIT 34 OFFSET 924;
INSERT INTO sessions (id, created_at, status) VALUES (413751, 'text 401532', now() + interval '22 days'), (911121, E'it''s\n', NULL)
  ON CONFLICT (id) DO UPDATE SET score = EXCLUDED.id RETURNING id;
UPDATE sessions SET owner_id = name + 23, region = CASE WHEN score > 991 THEN 'big' ELSE 'small' END WHERE id = $1 AND region <> ALL (ARRAY[6, 8, 7]);
WITH recent AS (SELECT * FROM products WHERE score > current_date - 12), ranked AS (
  SELECT *, row_number() OVER (PARTITION BY status ORDER BY owner_id DESC) AS rn FROM recent)
SELECT r.*, (SELECT max(id) FROM products x WHERE x.id = r.id) FROM ranked r WHERE rn <= 9
UNION ALL SELECT * FROM sessions WHERE EXISTS (SELECT 1 FROM products WHERE owner_id = $2);
DELETE FROM sessions USING orders WHERE orders.status = orders.id AND orders.score < now() - interval '1 year' RETURNING *;
SELECT a.score, b.created_at AS "B Col", count(*) FILTER (WHERE a.owner_id > 786) AS n, coalesce(sum(a.owner_id), 0)::numeric(12, 2)
  FROM events AS a LEFT JOIN invoices b ON b.id = a.region AND b.owner_id IS NOT NULL
  WHERE a.owner_id BETWEEN 82 AND 446 AND a.owner_id IN ('x', 'y', 'z') AND NOT a.region LIKE 'p%'
  GROUP BY 1, 2 HAVING count(*) > 1 ORDER BY n DESC, 1 LIMIT 13 OFFSET 347;
INSERT INTO orders (created_at, created_at, owner_id) VALUES (161709, 'text 138785', now() + interval '26 days'), (462032, E'it''s\n', NULL)
  ON CONFLICT (id) DO UPDATE SET amount = EXCLUDED.region RETURNING id;
UPDATE products SET region = score + 74, score = CASE WHEN score > 296 THEN 'big' ELSE 'small' END WHERE id = $1 AND amount <> ALL (ARRAY[6, 0, 3]);
WITH recent AS (SELECT * FROM events WHERE amount > current_date - 14), ranked AS (
  SELECT *, row_number() OVER (PARTITION BY created_at ORDER BY score DESC) AS rn FROM recent)
SELECT r.*, (SELECT max(name) FROM customers x WHERE x.id = r.id) FROM ranked r WHERE rn <= 0
UNION ALL SELECT * FROM invoices WHERE EXISTS (SELECT 1 FROM orders WHERE created_at = $2);
DELETE FROM products USING orders WHERE orders.name = orders.id AND orders.created_at < now() - interval '1 year' RETURNING *;
SELECT a.created_at, b.created_at AS "B Col", count(*) FILTER (WHERE a.created_at > 925) AS n, coalesce(sum(a.id), 0)::numeric(12, 2)
  FROM sessions AS a LEFT JOIN sessions b ON b.id = a.status AND b.status IS NOT NULL
  WHERE a.id BETWEEN 32 AND 868 AND a.owner_id IN ('x', 'y', 'z') AND NOT a.created_at LIKE 'p%'
  GROUP BY 1, 2 HAVING count(*) > 6 ORDER BY n DESC, 1 LIMIT 36 OFFSET 666;
INSERT INTO sessions (name, amount, amount) VALUES (582809, 'text 460410', now() + interval '0 days'), (734233, E'it''s\n', NULL)
  ON CONFLICT (id) DO UPDATE SET status = EXCLUDED.id RETURNING id;
UPDATE customers SET name = status + 91, region = CASE WHEN amount > 160 THEN 'big' ELSE 'small' END WHERE id = $1 AND name <> ALL (ARRAY[5, 4, 1]);
WITH recent AS (SELECT * FROM orders WHERE id > current_date - 78), ranked AS (
  SELECT *, row_number() OVER (PARTITION BY id ORDER BY region DESC) AS rn FROM recent)
SELECT r.*, (SELECT max(id) FROM orders x WHERE x.id = r.id) FROM ranked r WHERE rn <= 8
UNION ALL SELECT * FROM customers WHERE EXISTS (SELECT 1 FROM orders WHERE region = $2);
DELETE FROM sessions USING sessions WHERE orders.owner_id = orders.id AND orders.owner_id < now() - interval '1 year' RETURNING *;
SELECT a.region, b.status AS "B Col", count(*) FILTER (WHERE a.name > 418) AS n, coalesce(sum(a.name), 0)::numeric(12, 2)
  FROM products AS a LEFT JOIN customers b ON b.id = a.created_at AND b.name IS NOT NULL
  WHERE a.amount BETWEEN 41 AND 479 AND a.region IN ('x', 'y', 'z') AND NOT a.id LIKE 'p%'
  GROUP BY 1, 2 HAVING count(*) > 2 ORDER BY n DESC, 1 LIMIT 13 OFFSET 711;
INSERT INTO invoices (amount, name, score) VALUES (102757, 'text 534361', now() + interval '6 days'), (9444, E'it''s\n', NULL)
  ON CONFLICT (id) DO UPDATE SET status = EXCLUDED.score RETURNING id;
UPDATE products SET owner_id = created_at + 64, score = CASE WHEN amount > 630 THEN 'big' ELSE 'small' END WHERE id = $1 AND amount <> ALL (ARRAY[6, 6, 9]);
WITH recent AS (SELECT * FROM events WHERE score > current_date - 37), ranked AS (
  SELECT *, row_number() OVER (PARTITION BY name ORDER BY amount DESC) AS rn FROM recent)
SELECT r.*, (SELECT max(created_at) FROM events x WHERE x.id = r.id) FROM ranked r WHERE rn <= 8
UNION ALL SELECT * FROM events WHERE EXISTS (SELECT 1 FROM orders WHERE status = $2);
DELETE FROM sessions USING customers WHERE orders.score = orders.id AND orders.owner_id < now() - interval '1 year' RETURNING *;
SELECT a.created_at, b.owner_id AS "B Col", count(*) FILTER (WHERE a.id > 950) AS n, coalesce(sum(a.name), 0)::numeric(12, 2)
  FROM orders AS a LEFT JOIN customers b ON b.id = a.status AND b.id IS NOT NULL
  WHERE a.amount BETWEEN 46 AND 488 AND a.status IN ('x', 'y', 'z') AND NOT a.name LIKE 'p%'
  GROUP BY 1, 2 HAVING count(*) > 1 ORDER BY n DESC, 1 LIMIT 46 OFFSET 31;
INSERT INTO events (name, region, status) VALUES (249983, 'text 70195', now() + interval '2 days'), (522407, E'it''s\n', NULL)
  ON CONFLICT (id) DO UPDATE SET id = EXCLUDED.status RETURNING id;
UPDATE products SET owner_id = name + 97, region = CASE WHEN created_at > 171 THEN 'big' ELSE 'small' END WHERE id = $1 AND name <> ALL (ARRAY[2, 5, 1]);
WITH recent AS (SELECT * FROM customers WHERE status > current_date - 66), ranked AS (
  SELECT *, row_number() OVER (PARTITION BY id ORDER BY region DESC) AS rn FROM recent)
SELECT r.*, (SELECT max(status) FROM customers x WHERE x.id = r.id) FROM ranked r WHERE rn <= 5
UNION ALL SELECT * FROM invoices WHERE EXISTS (SELECT 1 FROM products WHERE status = $2);
DELETE FROM sessions USING orders WHERE orders.status = orders.id AND orders.score < now() - interval '1 year' RETURNING *;
SELECT a.amount, b.status AS "B Col", count(*) FILTER (WHERE a.status > 404) AS n, coalesce(sum(a.amount), 0)::numeric(12, 2)
  FROM products AS a LEFT JOIN sessions b ON b.id = a.score AND b.region IS NOT NULL
  WHERE a.score BETWEEN 3 AND 293 AND a.score IN ('x', 'y', 'z') AND NOT a.id LIKE 'p%'
  GROUP BY 1, 2 HAVING count(*) > 1 ORDER BY n DESC, 1 LIMIT 88 OFFSET 683;
INSERT INTO sessions (name, name, created_at) VALUES (141537, 'text 901754', now() + interval '3 days'), (855485, E'it''s\n', NULL)
  ON CONFLICT (id) DO UPDATE SET owner_id = EXCLUDED.amount RETURNING id;
UPDATE events SET status = id + 82, id = CASE WHEN score > 509 THEN 'big' ELSE 'small' END WHERE id = $1 AND owner_id <> ALL (ARRAY[9, 6, 9]);
WITH recent AS (SELECT * FROM products WHERE score > current_date - 61), ranked AS (
  SELECT *, row_number() OVER (PARTITION BY status ORDER BY name DESC) AS rn FROM recent)
SELECT r.*, (SELECT max(status) FROM sessions x WHERE x.id = r.id) FROM ranked r WHERE rn <= 0
UNION ALL SELECT * FROM events WHERE EXISTS (SELECT 1 FROM events WHERE status = $2);
DELETE FROM sessions USING events WHERE orders.name = orders.id AND orders.created_at < now() - interval '1 year' RETURNING *;
SELECT a.name, b.region AS "B Col", count(*) FILTER (WHERE a.status > 904) AS n, coalesce(sum(a.score), 0)::numeric(12, 2)
  FROM events AS a LEFT JOIN sessions b ON b.id = a.owner_id AND b.score IS NOT NULL
  WHERE a.owner_id BETWEEN 12 AND 929 AND a.id IN ('x', 'y', 'z') AND NOT a.score LIKE 'p%'
  GROUP BY 1, 2 HAVING count(*) > 7 ORDER BY n DESC, 1 LIMIT 62 OFFSET 932;
INSERT INTO invoices (status, id, name) VALUES (517912, 'text 351609', now() + interval '13 days'), (767490, E'it''s\n', NULL)
  ON CONFLICT (id) DO UPDATE SET owner_id = EXCLUDED.region RETURNING id;
UPDATE customers SET region = owner_id + 90, created_at = CASE WHEN score > 94 THEN 'big' ELSE 'small' END WHERE id = $1 AND amount <> ALL (ARRAY[8, 9, 6]);
WITH recent AS (SELECT * FROM products WHERE name > current_date - 39), ranked AS (
  SELECT *, row_number() OVER (PARTITION BY name ORDER BY score DESC) AS rn FROM recent)
SELECT r.*, (SELECT max(id) FROM customers x WHERE x.id = r.id) FROM ranked r WHERE rn <= 1
UNION ALL SELECT * FROM invoices WHERE EXISTS (SELECT 1 FROM events WHERE created_at = $2);
DELETE FROM invoices USING events WHERE orders.owner_id = orders.id AND orders.score < now() - interval '1 year' RETURNING *;
SELECT a.id, b.owner_id AS "B Col", count(*) FILTER (WHERE a.region > 109) AS n, coalesce(sum(a.id), 0)::numeric(12, 2)
  FROM customers AS a LEFT JOIN orders b ON b.id = a.amount AND b.name IS NOT NULL
  WHERE a.amount BETWEEN 18 AND 591 AND a.owner_id IN ('x', 'y', 'z') AND NOT a.score LIKE 'p%'
  GROUP BY 1, 2 HAVING count(*) > 3 ORDER BY n DESC, 1 LIMIT 23 OFFSET 556;
INSERT INTO customers (owner_id, region, amount) VALUES (880794, 'text 591165', now() + interval '5 days'), (112342, E'it''s\n', NULL)
  ON CONFLICT (id) DO UPDATE SET region = EXCLUDED.created_at RETURNING id;
UPDATE invoices SET amount = amount + 45, region = CASE WHEN name > 881 THEN 'big' ELSE 'small' END WHERE id = $1 AND score <> ALL (ARRAY[8, 8, 9]);
WITH recent AS (SELECT * FROM products WHERE owner_id > current_date - 70), ranked AS (
  SELECT *, row_number() OVER (PARTITION BY created_at ORDER BY created_at DESC) AS rn FROM recent)
SELECT r.*, (SELECT max(status) FROM customers x WHERE x.id = r.id) FROM ranked r WHERE rn <= 7
UNION ALL SELECT * FROM invoices WHERE EXISTS (SELECT 1 FROM sessions WHERE owner_id = $2);
DELETE FROM sessions USING invoices WHERE orders.amount = orders.id AND orders.status < now() - interval '1 year' RETURNING *;
SELECT a.created_at, b.owner_id AS "B Col", count(*) FILTER (WHERE a.id > 148) AS n, coalesce(sum(a.id), 0)::numeric(12, 2)
  FROM customers AS a LEFT JOIN events b ON b.id = a.score AND b.region IS NOT NULL
  WHERE a.amount BETWEEN 97 AND 700 AND a.id IN ('x', 'y', 'z') AND NOT a.score LIKE 'p%'
  GROUP BY 1, 2 HAVING count(*) > 0 ORDER BY n DESC, 1 LIMIT 43 OFFSET 660;
INSERT INTO sessions (id, amount, id) VALUES (369574, 'text 431812', now() + interval '3 days'), (204197, E'it''s\n', NULL)
  ON CONFLICT (id) DO UPDATE SET name = EXCLUDED.status RETURNING id;
UPDATE customers SET owner_id = owner_id + 69, status = CASE WHEN score > 778 THEN 'big' ELSE 'small' END WHERE id = $1 AND created_at <> ALL (ARRAY[9, 2, 7]);
WITH recent AS (SELECT * FROM sessions WHERE status > current_date - 87), ranked AS (
  SELECT *, row_number() OVER (PARTITION BY owner_id ORDER BY created_at DESC) AS rn FROM recent)
SELECT r.*, (SELECT max(created_at) FROM invoices x WHERE x.id = r.id) FROM ranked r WHERE rn <= 0
UNION ALL SELECT * FROM sessions WHERE EXISTS (SELECT 1 FROM events WHERE status = $2);
DELETE FROM orders USING orders WHERE orders.score = orders.id AND orders.name < now() - interval '1 year' RETURNING *;
SELECT a.name, b.name AS "B Col", count(*) FILTER (WHERE a.created_at > 59) AS n, coalesce(sum(a.id), 0)::numeric(12, 2)
  FROM sessions AS a LEFT JOIN customers b ON b.id = a.region AND b.name IS NOT NULL
  WHERE a.region BETWEEN 71 AND 116 AND a.owner_id IN ('x', 'y', 'z') AND NOT a.score LIKE 'p%'
  GROUP BY 1, 2 HAVING count(*) > 7 ORDER BY n DESC, 1 LIMIT 51 OFFSET 13;
INSERT INTO sessions (region, owner_id, created_at) VALUES (339207, 'text 279972', now() + interval '1 days'), (455790, E'it''s\n', NULL)
  ON CONFLICT (id) DO UPDATE SET region = EXCLUDED.id RETURNING id;
UPDATE customers SET name = amount + 80, status = CASE WHEN status > 112 THEN 'big' ELSE 'small' END WHERE id = $1 AND status <> ALL (ARRAY[6, 8, 2]);
WITH recent AS (SELECT * FROM orders WHERE id > current_date - 49), ranked AS (
  SELECT *, row_number() OVER (PARTITION BY name ORDER BY score DESC) AS rn FROM recent)
SELECT r.*, (SELECT max(id) FROM orders x WHERE x.id = r.id) FROM ranked r WHERE rn <= 2
UNION ALL SELECT * FROM events WHERE EXISTS (SELECT 1 FROM customers WHERE status = $2);
DELETE FROM sessions USING products WHERE orders.score = orders.id AND orders.score < now() - interval '1 year' RETURNING *;
SELECT a.amount, b.id AS "B Col", count(*) FILTER (WHERE a.status > 890) AS n, coalesce(sum(a.id), 0)::numeric(12, 2)
  FROM customers AS a LEFT JOIN sessions b ON b.id = a.name AND b.region IS NOT NULL
  WHERE a.status BETWEEN 37 AND 523 AND a.name IN ('x', 'y', 'z') AND NOT a.name LIKE 'p%'
  GROUP BY 1, 2 HAVING count(*) > 5 ORDER BY n DESC, 1 LIMIT 40 OFFSET 788;
INSERT INTO sessions (region, owner_id, owner_id) VALUES (623271, 'text 676639', now() + interval '26 days'), (926810, E'it''s\n', NULL)
  ON CONFLICT (id) DO UPDATE SET created_at = EXCLUDED.owner_id RETURNING id;
UPDATE customers SET name = created_at + 36, owner_id = CASE WHEN score > 902 THEN 'big' ELSE 'small' END WHERE id = $1 AND status <> ALL (ARRAY[3, 6, 2]);
WITH recent AS (SELECT * FROM invoices WHERE name > current_date - 65), ranked AS (
  SELECT *, row_number() OVER (PARTITION BY score ORDER BY name DESC) AS rn FROM recent)
SELECT r.*, (SELECT max(name) FROM invoices x WHERE x.id = r.id) FROM ranked r WHERE rn <= 3
UNION ALL SELECT * FROM invoices WHERE EXISTS (SELECT 1 FROM products WHERE amount = $2);
DELETE FROM products USING events WHERE orders.region = orders.id AND orders.status < now() - interval '1 year' RETURNING *;
SELECT a.id, b.amount AS "B Col", count(*) FILTER (WHERE a.amount > 379) AS n, coalesce(sum(a.id), 0)::numeric(12, 2)
  FROM events AS a LEFT JOIN customers b ON b.id = a.owner_id AND b.amount IS NOT NULL
  WHERE a.status BETWEEN 75 AND 1043 AND a.id IN ('x', 'y', 'z') AND NOT a.owner_id LIKE 'p%'
  GROUP BY 1, 2 HAVING count(*) > 1 ORDER BY n DESC, 1 LIMIT 31 OFFSET 162;
INSERT INTO products (region, id, created_at) VALUES (292127, 'text 665802', now() + interval '16 days'), (819118, E'it''s\n', NULL)
  ON CONFLICT (id) DO UPDATE SET name = EXCLUDED.region RETURNING id;
UPDATE invoices SET score = name + 94, score = CASE WHEN score > 210 THEN 'big' ELSE 'small' END WHERE id = $1 AND amount <> ALL (ARRAY[9, 0, 7]);
WITH recent AS (SELECT * FROM products WHERE name > current_date - 9), ranked AS (
  SELECT *, row_number() OVER (PARTITION BY id ORDER BY region DESC) AS rn FROM recent)
SELECT r.*, (SELECT max(owner_id) FROM customers x WHERE x.id = r.id) FROM ranked r WHERE rn <= 5
UNION ALL SELECT * FROM products WHERE EXISTS (SELECT 1 FROM products WHERE region = $2);
DELETE FROM products USING events WHERE orders.amount = orders.id AND orders.owner_id < now() - interval '1 year' RETURNING *;
SELECT a.name, b.name AS "B Col", count(*) FILTER (WHERE a.id > 720) AS n, coalesce(sum(a.score), 0)::numeric(12, 2)
  FROM products AS a LEFT JOIN sessions b ON b.id = a.region AND b.created_at IS NOT NULL
  WHERE a.owner_id BETWEEN 21 AND 150 AND a.status IN ('x', 'y', 'z') AND NOT a.created_at LIKE 'p%'
  GROUP BY 1, 2 HAVING count(*) > 1 ORDER BY n DESC, 1 LIMIT 45 OFFSET 240;
INSERT INTO orders (id, id, name) VALUES (608598, 'text 779811', now() + interval '28 days'), (939320, E'it''s\n', NULL)
  ON CONFLICT (id) DO UPDATE SET id = EXCLUDED.name RETURNING id;
UPDATE orders SET status = owner_id + 19, owner_id = CASE WHEN region > 408 THEN 'big' ELSE 'small' END WHERE id = $1 AND id <> ALL (ARRAY[0, 9, 6]);
WITH recent AS (SELECT * FROM products WHERE region > current_date - 51), ranked AS (
  SELECT *, row_number() OVER (PARTITION BY status ORDER BY id DESC) AS rn FROM recent)
SELECT r.*, (SELECT max(region) FROM invoices x WHERE x.id = r.id) FROM ranked r WHERE rn <= 7
UNION ALL SELECT * FROM sessions WHERE EXISTS (SELECT 1 FROM sessions WHERE owner_id = $2);
DELETE FROM events USING events WHERE orders.name = orders.id AND orders.created_at < now() - interval '1 year' RETURNING *;
SELECT a.region, b.owner_id AS "B Col", count(*) FILTER (WHERE a.id > 675) AS n, coalesce(sum(a.score), 0)::numeric(12, 2)
  FROM customers AS a LEFT JOIN events b ON b.id = a.created_at AND b.region IS NOT NULL
  WHERE a.score BETWEEN 82 AND 1095 AND a.score IN ('x', 'y', 'z') AND NOT a.score LIKE 'p%'
  GROUP BY 1, 2 HAVING count(*) > 0 ORDER BY n DESC, 1 LIMIT 84 OFFSET 558;
INSERT INTO invoices (status, status, owner_id) VALUES (279185, 'text 113014', now() + interval '5 days'), (452560, E'it''s\n', NULL)
  ON CONFLICT (id) DO UPDATE SET amount = EXCLUDED.id RETURNING id;
UPDATE orders SET score = amount + 71, score = CASE WHEN id > 572 THEN 'big' ELSE 'small' END WHERE id = $1 AND amount <> ALL (ARRAY[7, 3, 7]);
WITH recent AS (SELECT * FROM events WHERE score > current_date - 66), ranked AS (
  SELECT *, row_number() OVER (PARTITION BY score ORDER BY status DESC) AS rn FROM recent)
SELECT r.*, (SELECT max(amount) FROM sessions x WHERE x.id = r.id) FROM ranked r WHERE rn <= 4
UNION ALL SELECT * FROM events WHERE EXISTS (SELECT 1 FROM invoices WHERE amount = $2);
DELETE FROM orders USING sessions WHERE orders.status = orders.id AND orders.score < now() - interval '1 year' RETURNING *;
SELECT a.amount, b.score AS "B Col", count(*) FILTER (WHERE a.score > 826) AS n, coalesce(sum(a.id), 0)::numeric(12, 2)
  FROM orders AS a LEFT JOIN orders b ON b.id = a.region AND b.created_at IS NOT NULL
  WHERE a.name BETWEEN 77 AND 928 AND a.score IN ('x', 'y', 'z') AND NOT a.name LIKE 'p%'
  GROUP BY 1, 2 HAVING count(*) > 1 ORDER BY n DESC, 1 LIMIT 73 OFFSET 52;
INSERT INTO products (region, amount, created_at) VALUES (198029, 'text 365307', now() + interval '5 days'), (939125, E'it''s\n', NULL)
  ON CONFLICT (id) DO UPDATE SET status = EXCLUDED.name RETURNING id;
UPDATE products SET id = status + 87, status = CASE WHEN owner_id > 611 THEN 'big' ELSE 'small' END WHERE id = $1 AND region <> ALL (ARRAY[7, 8, 0]);
WITH recent AS (SELECT * FROM invoices WHERE id > current_date - 25), ranked AS (
  SELECT *, row_number() OVER (PARTITION BY amount ORDER BY id DESC) AS rn FROM recent)
SELECT r.*, (SELECT max(created_at) FROM products x WHERE x.id = r.id) FROM ranked r WHERE rn <= 1
UNION ALL SELECT * FROM products WHERE EXISTS (SELECT 1 FROM products WHERE status = $2);
DELETE FROM events USING orders WHERE orders.owner_id = orders.id AND orders.amount < now() - interval '1 year' RETURNING *;
SELECT a.status, b.amount AS "B Col", count(*) FILTER (WHERE a.amount > 729) AS n, coalesce(sum(a.status), 0)::numeric(12, 2)
  FROM sessions AS a LEFT JOIN orders b ON b.id = a.amount AND b.score IS NOT NULL
  WHERE a.owner_id BETWEEN 85 AND 183 AND a.owner_id IN ('x', 'y', 'z') AND NOT a.score LIKE 'p%'
  GROUP BY 1, 2 HAVING count(*) > 6 ORDER BY n DESC, 1 LIMIT 43 OFFSET 968;
INSERT INTO invoices (region, status, score) VALUES (252617, 'text 819660', now() + interval '12 days'), (324645, E'it''s\n', NULL)
  ON CONFLICT (id) DO UPDATE SET score = EXCLUDED.name RETURNING id;
UPDATE invoices SET status = region + 50, region = CASE WHEN region > 449 THEN 'big' ELSE 'small' END WHERE id = $1 AND region <> ALL (ARRAY[3, 6, 1]);
WITH recent AS (SELECT * FROM invoices WHERE score > current_date - 47), ranked AS (
  SELECT *, row_number() OVER (PARTITION BY name ORDER BY created_at DESC) AS rn FROM recent)
SELECT r.*, (SELECT max(score) FROM orders x WHERE x.id = r.id) FROM ranked r WHERE rn <= 4
UNION ALL SELECT * FROM customers WHERE EXISTS (SELECT 1 FROM products WHERE id = $2);
DELETE FROM sessions USING products WHERE orders.status = orders.id AND orders.created_at < now() - interval '1 year' RETURNING *;
SELECT a.name, b.amount AS "B Col", count(*) FILTER (WHERE a.owner_id > 530) AS n, coalesce(sum(a.created_at), 0)::numeric(12, 2)
  FROM invoices AS a LEFT JOIN customers b ON b.id = a.name AND b.region IS NOT NULL
  WHERE a.status BETWEEN 4 AND 805 AND a.score IN ('x', 'y', 'z') AND NOT a.created_at LIKE 'p%'
  GROUP BY 1, 2 HAVING count(*) > 1 ORDER BY n DESC, 1 LIMIT 9 OFFSET 594;
INSERT INTO orders (created_at, name, amount) VALUES (994783, 'text 210174', now() + interval '26 days'), (433428, E'it''s\n', NULL)
  ON CONFLICT (id) DO UPDATE SET owner_id = EXCLUDED.owner_id RETURNING id;
UPDATE invoices SET status = score + 71, id = CASE WHEN amount > 792 THEN 'big' ELSE 'small' END WHERE id = $1 AND id <> ALL (ARRAY[3, 0, 1]);
WITH recent AS (SELECT * FROM invoices WHERE score > current_date - 69), ranked AS (
  SELECT *, row_number() OVER (PARTITION BY region ORDER BY score DESC) AS rn FROM recent)
SELECT r.*, (SELECT max(status) FROM orders x WHERE x.id = r.id) FROM ranked r WHERE rn <= 6
UNION ALL SELECT * FROM invoices WHERE EXISTS (SELECT 1 FROM customers WHERE score = $2);
DELETE FROM sessions USING events WHERE orders.score = orders.id AND orders.created_at < now() - interval '1 year' RETURNING *;
SELECT a.region, b.amount AS "B Col", count(*) FILTER (WHERE a.region > 976) AS n, coalesce(sum(a.amount), 0)::numeric(12, 2)
  FROM orders AS a LEFT JOIN sessions b ON b.id = a.region AND b.status IS NOT NULL
  WHERE a.status BETWEEN 16 AND 1057 AND a.status IN ('x', 'y', 'z') AND NOT a.name LIKE 'p%'
  GROUP BY 1, 2 HAVING count(*) > 8 ORDER BY n DESC, 1 LIMIT 14 OFFSET 140;
INSERT INTO sessions (score, region, owner_id) VALUES (175780, 'text 916783', now() + interval '20 days'), (98222, E'it''s\n', NULL)
  ON CONFLICT (id) DO UPDATE SET status = EXCLUDED.id RETURNING id;
UPDATE invoices SET status = created_at + 31, status = CASE WHEN created_at > 305 THEN 'big' ELSE 'small' END WHERE id = $1 AND created_at <> ALL (ARRAY[6, 1, 2]);
WITH recent AS (SELECT * FROM sessions WHERE created_at > current_date - 47), ranked AS (
  SELECT *, row_number() OVER (PARTITION BY score ORDER BY score DESC) AS rn FROM recent)
SELECT r.*, (SELECT max(score) FROM customers x WHERE x.id = r.id) FROM ranked r WHERE rn <= 3
UNION ALL SELECT * FROM events WHERE EXISTS (SELECT 1 FROM invoices WHERE name = $2);
DELETE FROM invoices USING products WHERE orders.name = orders.id AND orders.region < now() - interval '1 year' RETURNING *;
SELECT a.score, b.name AS "B Col", count(*) FILTER (WHERE a.region > 628) AS n, coalesce(sum(a.score), 0)::numeric(12, 2)
  FROM sessions AS a LEFT JOIN sessions b ON b.id = a.created_at AND b.id IS NOT NULL
  WHERE a.created_at BETWEEN 44 AND 389 AND a.id IN ('x', 'y', 'z') AND NOT a.id LIKE 'p%'
  GROUP BY 1, 2 HAVING count(*) > 6 ORDER BY n DESC, 1 LIMIT 83 OFFSET 617;
INSERT INTO orders (score, score, score) VALUES (66325, 'text 361378', now() + interval '26 days'), (755568, E'it''s\n', NULL)
  ON CONFLICT (id) DO UPDATE SET created_at = EXCLUDED.name RETURNING id;
UPDATE customers SET name = score + 24, name = CASE WHEN score > 972 THEN 'big' ELSE 'small' END WHERE id = $1 AND created_at <> ALL (ARRAY[4, 1, 5]);
WITH recent AS (SELECT * FROM events WHERE id > current_date - 13), ranked AS (
  SELECT *, row_number() OVER (PARTITION BY name ORDER BY amount DESC) AS rn FROM recent)
SELECT r.*, (SELECT max(name) FROM events x WHERE x.id = r.id) FROM ranked r WHERE rn <= 4
UNION ALL SELECT * FROM products WHERE EXISTS (SELECT 1 FROM sessions WHERE status = $2);
DELETE FROM invoices USING sessions WHERE orders.created_at = orders.id AND orders.status < now() - interval '1 year' RETURNING *;
SELECT a.created_at, b.id AS "B Col", count(*) FILTER (WHERE a.region > 49) AS n, coalesce(sum(a.status), 0)::numeric(12, 2)
  FROM customers AS a LEFT JOIN orders b ON b.id = a.name AND b.region IS NOT NULL
  WHERE a.region BETWEEN 79 AND 413 AND a.amount IN ('x', 'y', 'z') AND NOT a.region LIKE 'p%'
  GROUP BY 1, 2 HAVING count(*) > 9 ORDER BY n DESC, 1 LIMIT 39 OFFSET 191;
INSERT INTO products (created_at, score, name) VALUES (648393, 'text 716484', now() + interval '9 days'), (137729, E'it''s\n', NULL)
  ON CONFLICT (id) DO UPDATE SET id = EXCLUDED.amount RETURNING id;
UPDATE customers SET amount = status + 31, id = CASE WHEN name > 605 THEN 'big' ELSE 'small' END WHERE id = $1 AND status <> ALL (ARRAY[4, 1, 6]);
WITH recent AS (SELECT * FROM orders WHERE amount > current_date - 12), ranked AS (
  SELECT *, row_number() OVER (PARTITION BY amount ORDER BY region DESC) AS rn FROM recent)
SELECT r.*, (SELECT max(score) FROM products x WHERE x.id = r.id) FROM ranked r WHERE rn <= 9
UNION ALL SELECT * FROM products WHERE EXISTS (SELECT 1 FROM sessions WHERE name = $2);
DELETE FROM invoices USING sessions WHERE orders.status = orders.id AND orders.region < now() - interval '1 year' RETURNING *;
SELECT a.id, b.status AS "B Col", count(*) FILTER (WHERE a.owner_id > 572) AS n, coalesce(sum(a.owner_id), 0)::numeric(12, 2)
  FROM events AS a LEFT JOIN sessions b ON b.id = a.amount AND b.status IS NOT NULL
  WHERE a.created_at BETWEEN 4 AND 127 AND a.score IN ('x', 'y', 'z') AND NOT a.name LIKE 'p%'
  GROUP BY 1, 2 HAVING count(*) > 1 ORDER BY n DESC, 1 LIMIT 57 OFFSET 285;
INSERT INTO invoices (id, name, name) VALUES (822564, 'text 624088', now() + interval '29 days'), (353968, E'it''s\n', NULL)
  ON CONFLICT (id) DO UPDATE SET owner_id = EXCLUDED.region RETURNING id;
UPDATE invoices SET status = owner_id + 66, created_at = CASE WHEN id > 797 THEN 'big' ELSE 'small' END WHERE id = $1 AND score <> ALL (ARRAY[1, 0, 6]);
WITH recent AS (SELECT * FROM customers WHERE region > current_date - 1), ranked AS (
  SELECT *, row_number() OVER (PARTITION BY score ORDER BY name DESC) AS rn FROM recent)
SELECT r.*, (SELECT max(amount) FROM events x WHERE x.id = r.id) FROM ranked r WHERE rn <= 6
UNION ALL SELECT * FROM orders WHERE EXISTS (SELECT 1 FROM customers WHERE owner_id = $2);
DELETE FROM events USING invoices WHERE orders.score = orders.id AND orders.region < now() - interval '1 year' RETURNING *;
SELECT a.amount, b.id AS "B Col", count(*) FILTER (WHERE a.score > 215) AS n, coalesce(sum(a.region), 0)::numeric(12, 2)
  FROM events AS a LEFT JOIN customers b ON b.id = a.name AND b.name IS NOT NULL
  WHERE a.status BETWEEN 8 AND 422 AND a.id IN ('x', 'y', 'z') AND NOT a.status LIKE 'p%'
  GROUP BY 1, 2 HAVING count(*) > 4 ORDER BY n DESC, 1 LIMIT 14 OFFSET 33;
INSERT INTO events (id, amount, created_at) VALUES (779181, 'text 987230', now() + interval '18 days'), (365759, E'it''s\n', NULL)
  ON CONFLICT (id) DO UPDATE SET owner_id = EXCLUDED.amount RETURNING id;
UPDATE products SET owner_id = id + 4, amount = CASE WHEN score > 156 THEN 'big' ELSE 'small' END WHERE id = $1 AND id <> ALL (ARRAY[6, 7, 7]);
WITH recent AS (SELECT * FROM customers WHERE amount > current_date - 89), ranked AS (
  SELECT *, row_number() OVER (PARTITION BY amount ORDER BY id DESC) AS rn FROM recent)
SELECT r.*, (SELECT max(region) FROM customers x WHERE x.id = r.id) FROM ranked r WHERE rn <= 7
UNION ALL SELECT * FROM orders WHERE EXISTS (SELECT 1 FROM customers WHERE region = $2);
DELETE FROM orders USING products WHERE orders.region = orders.id AND orders.score < now() - interval '1 year' RETURNING *;
SELECT a.name, b.amount AS "B Col", count(*) FILTER (WHERE a.status > 766) AS n, coalesce(sum(a.region), 0)::numeric(12, 2)
  FROM orders AS a LEFT JOIN events b ON b.id = a.name AND b.id IS NOT NULL
  WHERE a.created_at BETWEEN 20 AND 266 AND a.amount IN ('x', 'y', 'z') AND NOT a.id LIKE 'p%'
  GROUP BY 1, 2 HAVING count(*) > 3 ORDER BY n DESC, 1 LIMIT 77 OFFSET 99;
INSERT INTO events (created_at, amount, region) VALUES (905928, 'text 676852', now() + interval '25 days'), (110127, E'it''s\n', NULL)
  ON CONFLICT (id) DO UPDATE SET name = EXCLUDED.status RETURNING id;
UPDATE events SET id = status + 0, amount = CASE WHEN id > 190 THEN 'big' ELSE 'small' END WHERE id = $1 AND id <> ALL (ARRAY[2, 5, 1]);
WITH recent AS (SELECT * FROM events WHERE owner_id > current_date - 75), ranked AS (
  SELECT *, row_number() OVER (PARTITION BY owner_id ORDER BY id DESC) AS rn FROM recent)
SELECT r.*, (SELECT max(created_at) FROM products x WHERE x.id = r.id) FROM ranked r WHERE rn <= 7
UNION ALL SELECT * FROM products WHERE EXISTS (SELECT 1 FROM sessions WHERE region = $2);
DELETE FROM products USING customers WHERE orders.status = orders.id AND orders.status < now() - interval '1 year' RETURNING *;
SELECT a.region, b.status AS "B Col", count(*) FILTER (WHERE a.owner_id > 84) AS n, coalesce(sum(a.owner_id), 0)::numeric(12, 2)
  FROM sessions AS a LEFT JOIN orders b ON b.id = a.status AND b.status IS NOT NULL
  WHERE a.name BETWEEN 94 AND 743 AND a.amount IN ('x', 'y', 'z') AND NOT a.id LIKE 'p%'
  GROUP BY 1, 2 HAVING count(*) > 2 ORDER BY n DESC, 1 LIMIT 56 OFFSET 121;
INSERT INTO sessions (score, status, name) VALUES (548344, 'text 197431', now() + interval '11 days'), (27556, E'it''s\n', NULL)
  ON CONFLICT (id) DO UPDATE SET region = EXCLUDED.status RETURNING id;
UPDATE events SET status = id + 93, region = CASE WHEN owner_id > 367 THEN 'big' ELSE 'small' END WHERE id = $1 AND id <> ALL (ARRAY[7, 4, 8]);
WITH recent AS (SELECT * FROM events WHERE status > current_date - 9), ranked AS (
  SELECT *, row_number() OVER (PARTITION BY owner_id ORDER BY amount DESC) AS rn FROM recent)
SELECT r.*, (SELECT max(owner_id) FROM invoices x WHERE x.id = r.id) FROM ranked r WHERE rn <= 7
UNION ALL SELECT * FROM customers WHERE EXISTS (SELECT 1 FROM orders WHERE name = $2);
DELETE FROM invoices USING sessions WHERE orders.status = orders.id AND orders.region < now() - interval '1 year' RETURNING *;
SELECT a.owner_id, b.status AS "B Col", count(*) FILTER (WHERE a.created_at > 431) AS n, coalesce(sum(a.id), 0)::numeric(12, 2)
  FROM invoices AS a LEFT JOIN sessions b ON b.id = a.amount AND b.id IS NOT NULL
  WHERE a.region BETWEEN 93 AND 322 AND a.name IN ('x', 'y', 'z') AND NOT a.name LIKE 'p%'
  GROUP BY 1, 2 HAVING count(*) > 7 ORDER BY n DESC, 1 LIMIT 61 OFFSET 468;
INSERT INTO sessions (owner_id, amount, owner_id) VALUES (577548, 'text 214579', now() + interval '3 days'), (288408, E'it''s\n', NULL)
  ON CONFLICT (id) DO UPDATE SET created_at = EXCLUDED.owner_id RETURNING id;
UPDATE orders SET owner_id = amount + 80, name = CASE WHEN id > 972 THEN 'big' ELSE 'small' END WHERE id = $1 AND name <> ALL (ARRAY[9, 3, 5]);
WITH recent AS (SELECT * FROM products WHERE region > current_date - 77), ranked AS (
  SELECT *, row_number() OVER (PARTITION BY id ORDER BY created_at DESC) AS rn FROM recent)
SELECT r.*, (SELECT max(status) FROM orders x WHERE x.id = r.id) FROM ranked r WHERE rn <= 4
UNION ALL SELECT * FROM sessions WHERE EXISTS (SELECT 1 FROM events WHERE status = $2);
DELETE FROM orders USING products WHERE orders.owner_id = orders.id AND orders.status < now() - interval '1 year' RETURNING *;
SELECT a.amount, b.name AS "B Col", count(*) FILTER (WHERE a.region > 276) AS n, coalesce(sum(a.amount), 0)::numeric(12, 2)
  FROM customers AS a LEFT JOIN products b ON b.id = a.score AND b.name IS NOT NULL
  WHERE a.amount BETWEEN 60 AND 501 AND a.owner_id IN ('x', 'y', 'z') AND NOT a.id LIKE 'p%'
  GROUP BY 1, 2 HAVING count(*) > 4 ORDER BY n DESC, 1 LIMIT 22 OFFSET 742;
INSERT INTO orders (status, amount, amount) VALUES (552194, 'text 626227', now() + interval '12 days'), (546485, E'it''s\n', NULL)
  ON CONFLICT (id) DO UPDATE SET created_at = EXCLUDED.created_at RETURNING id;
UPDATE customers SET status = region + 6, score = CASE WHEN name > 60 THEN 'big' ELSE 'small' END WHERE id = $1 AND created_at <> ALL (ARRAY[0, 2, 4]);
WITH recent AS (SELECT * FROM customers WHERE amount > current_date - 1), ranked AS (
  SELECT *, row_number() OVER (PARTITION BY created_at ORDER BY amount DESC) AS rn FROM recent)
SELECT r.*, (SELECT max(id) FROM events x WHERE x.id = r.id) FROM ranked r WHERE rn <= 8
UNION ALL SELECT * FROM products WHERE EXISTS (SELECT 1 FROM products WHERE region = $2);
DELETE FROM invoices USING sessions WHERE orders.name = orders.id AND orders.region < now() - interval '1 year' RETURNING *;
SELECT a.status, b.created_at AS "B Col", count(*) FILTER (WHERE a.owner_id > 408) AS n, coalesce(sum(a.status), 0)::numeric(12, 2)
  FROM sessions AS a LEFT JOIN invoices b ON b.id = a.amount AND b.status IS NOT NULL
  WHERE a.created_at BETWEEN 53 AND 275 AND a.score IN ('x', 'y', 'z') AND NOT a.region LIKE 'p%'
  GROUP BY 1, 2 HAVING count(*) > 3 ORDER BY n DESC, 1 LIMIT 56 OFFSET 101;
INSERT INTO sessions (created_at, status, region) VALUES (913910, 'text 291453', now() + interval '23 days'), (900770, E'it''s\n', NULL)
  ON CONFLICT (id) DO UPDATE SET id = EXCLUDED.status RETURNING id;
UPDATE sessions SET created_at = region + 46, owner_id = CASE WHEN id > 927 THEN 'big' ELSE 'small' END WHERE id = $1 AND status <> ALL (ARRAY[1, 2, 5]);
WITH recent AS (SELECT * FROM events WHERE status > current_date - 12), ranked AS (
  SELECT *, row_number() OVER (PARTITION BY region ORDER BY region DESC) AS rn FROM recent)
SELECT r.*, (SELECT max(score) FROM invoices x WHERE x.id = r.id) FROM ranked r WHERE rn <= 4
UNION ALL SELECT * FROM events WHERE EXISTS (SELECT 1 FROM invoices WHERE region = $2);
DELETE FROM invoices USING products WHERE orders.region = orders.id AND orders.region < now() - interval '1 year' RETURNING *;
SELECT a.created_at, b.region AS "B Col", count(*) FILTER (WHERE a.status > 601) AS n, coalesce(sum(a.id), 0)::numeric(12, 2)
  FROM orders AS a LEFT JOIN invoices b ON b.id = a.created_at AND b.owner_id IS NOT NULL
  WHERE a.status BETWEEN 44 AND 260 AND a.score IN ('x', 'y', 'z') AND NOT a.region LIKE 'p%'
  GROUP BY 1, 2 HAVING count(*) > 3 ORDER BY n DESC, 1 LIMIT 45 OFFSET 460;
INSERT INTO orders (amount, score, id) VALUES (305962, 'text 123937', now() + interval '4 days'), (550569, E'it''s\n', NULL)
  ON CONFLICT (id) DO UPDATE SET owner_id = EXCLUDED.amount RETURNING id;
UPDATE orders SET amount = created_at + 45, owner_id = CASE WHEN region > 824 THEN 'big' ELSE 'small' END WHERE id = $1 AND status <> ALL (ARRAY[3, 1, 6]);
WITH recent AS (SELECT * FROM invoices WHERE region > current_date - 44), ranked AS (
  SELECT *, row_number() OVER (PARTITION BY name ORDER BY created_at DESC) AS rn FROM recent)
SELECT r.*, (SELECT max(id) FROM invoices x WHERE x.id = r.id) FROM ranked r WHERE rn <= 5
UNION ALL SELECT * FROM customers WHERE EXISTS (SELECT 1 FROM events WHERE region = $2);
DELETE FROM customers USING orders WHERE orders.status = orders.id AND orders.score < now() - interval '1 year' RETURNING *;
SELECT a.amount, b.id AS "B Col", count(*) FILTER (WHERE a.status > 983) AS n, coalesce(sum(a.status), 0)::numeric(12, 2)
  FROM products AS a LEFT JOIN orders b ON b.id = a.created_at AND b.score IS NOT NULL
  WHERE a.created_at BETWEEN 20 AND 973 AND a.id IN ('x', 'y', 'z') AND NOT a.score LIKE 'p%'
  GROUP BY 1, 2 HAVING count(*) > 3 ORDER BY n DESC, 1 LIMIT 78 OFFSET 268;
INSERT INTO products (id, region, status) VALUES (9384, 'text 629955', now() + interval '17 days'), (723034, E'it''s\n', NULL)
  ON CONFLICT (id) DO UPDATE SET region = EXCLUDED.status RETURNING id;
UPDATE orders SET region = status + 80, score = CASE WHEN created_at > 830 THEN 'big' ELSE 'small' END WHERE id = $1 AND region <> ALL (ARRAY[0, 8, 7]);
WITH recent AS (SELECT * FROM sessions WHERE region > current_date - 35), ranked AS (
  SELECT *, row_number() OVER (PARTITION BY id ORDER BY id DESC) AS rn FROM recent)
SELECT r.*, (SELECT max(owner_id) FROM invoices x WHERE x.id = r.id) FROM ranked r WHERE rn <= 5
UNION ALL SELECT * FROM products WHERE EXISTS (SELECT 1 FROM products WHERE region = $2);
DELETE FROM products USING customers WHERE orders.created_at = orders.id AND orders.status < now() - interval '1 year' RETURNING *;
SELECT a.id, b.amount AS "B Col", count(*) FILTER (WHERE a.owner_id > 112) AS n, coalesce(sum(a.created_at), 0)::numeric(12, 2)
  FROM invoices AS a LEFT JOIN sessions b ON b.id = a.created_at AND b.amount IS NOT NULL
  WHERE a.owner_id BETWEEN 42 AND 895 AND a.created_at IN ('x', 'y', 'z') AND NOT a.status LIKE 'p%'
  GROUP BY 1, 2 HAVING count(*) > 8 ORDER BY n DESC, 1 LIMIT 36 OFFSET 932;
INSERT INTO events (created_at, id, score) VALUES (603886, 'text 645792', now() + interval '14 days'), (443505, E'it''s\n', NULL)
  ON CONFLICT (id) DO UPDATE SET created_at = EXCLUDED.score RETURNING id;
UPDATE orders SET id = id + 81, id = CASE WHEN region > 485 THEN 'big' ELSE 'small' END WHERE id = $1 AND id <> ALL (ARRAY[1, 7, 3]);
WITH recent AS (SELECT * FROM products WHERE status > current_date - 28), ranked AS (
  SELECT *, row_number() OVER (PARTITION BY owner_id ORDER BY score DESC) AS rn FROM recent)
SELECT r.*, (SELECT max(score) FROM sessions x WHERE x.id = r.id) FROM ranked r WHERE rn <= 8
UNION ALL SELECT * FROM events WHERE EXISTS (SELECT 1 FROM sessions WHERE amount = $2);
DELETE FROM events USING customers WHERE orders.id = orders.id AND orders.status < now() - interval '1 year' RETURNING *;
SELECT a.id, b.owner_id AS "B Col", count(*) FILTER (WHERE a.score > 68) AS n, coalesce(sum(a.owner_id), 0)::numeric(12, 2)
  FROM orders AS a LEFT JOIN products b ON b.id = a.score AND b.amount IS NOT NULL
  WHERE a.name BETWEEN 56 AND 947 AND a.region IN ('x', 'y', 'z') AND NOT a.name LIKE 'p%'
  GROUP BY 1, 2 HAVING count(*) > 2 ORDER BY n DESC, 1 LIMIT 74 OFFSET 69;
INSERT INTO invoices (score, name, score) VALUES (434067, 'text 877939', now() + interval '19 days'), (79969, E'it''s\n', NULL)
  ON CONFLICT (id) DO UPDATE SET status = EXCLUDED.name RETURNING id;
UPDATE products SET amount = amount + 58, region = CASE WHEN created_at > 766 THEN 'big' ELSE 'small' END WHERE id = $1 AND score <> ALL (ARRAY[8, 0, 6]);
WITH recent AS (SELECT * FROM customers WHERE name > current_date - 21), ranked AS (
  SELECT *, row_number() OVER (PARTITION BY created_at ORDER BY score DESC) AS rn FROM recent)
SELECT r.*, (SELECT max(name) FROM sessions x WHERE x.id = r.id) FROM ranked r WHERE rn <= 9
UNION ALL SELECT * FROM products WHERE EXISTS (SELECT 1 FROM sessions WHERE owner_id = $2);
DELETE FROM invoices USING events WHERE orders.name = orders.id AND orders.id < now() - interval '1 year' RETURNING *;
SELECT a.score, b.status AS "B Col", count(*) FILTER (WHERE a.id > 20) AS n, coalesce(sum(a.owner_id), 0)::numeric(12, 2)
  FROM orders AS a LEFT JOIN products b ON b.id = a.owner_id AND b.owner_id IS NOT NULL
  WHERE a.id BETWEEN 39 AND 509 AND a.status IN ('x', 'y', 'z') AND NOT a.owner_id LIKE 'p%'
  GROUP BY 1, 2 HAVING count(*) > 6 ORDER BY n DESC, 1 LIMIT 65 OFFSET 319;
INSERT INTO products (created_at, created_at, amount) VALUES (338292, 'text 588459', now() + interval '5 days'), (688228, E'it''s\n', NULL)
  ON CONFLICT (id) DO UPDATE SET region = EXCLUDED.amount RETURNING id;
UPDATE events SET status = region + 73, amount = CASE WHEN region > 878 THEN 'big' ELSE 'small' END WHERE id = $1 AND created_at <> ALL (ARRAY[3, 3, 1]);
WITH recent AS (SELECT * FROM products WHERE score > current_date - 5), ranked AS (
  SELECT *, row_number() OVER (PARTITION BY name ORDER BY region DESC) AS rn FROM recent)
SELECT r.*, (SELECT max(amount) FROM sessions x WHERE x.id = r.id) FROM ranked r WHERE rn <= 2
UNION ALL SELECT * FROM customers WHERE EXISTS (SELECT 1 FROM customers WHERE name = $2);
DELETE FROM invoices USING events WHERE orders.owner_id = orders.id AND orders.amount < now() - interval '1 year' RETURNING *;
SELECT a.id, b.name AS "B Col", count(*) FILTER (WHERE a.id > 583) AS n, coalesce(sum(a.status), 0)::numeric(12, 2)
  FROM events AS a LEFT JOIN customers b ON b.id = a.id AND b.status IS NOT NULL
  WHERE a.region BETWEEN 32 AND 762 AND a.created_at IN ('x', 'y', 'z') AND NOT a.status LIKE 'p%'
  GROUP BY 1, 2 HAVING count(*) > 5 ORDER BY n DESC, 1 LIMIT 95 OFFSET 264;
INSERT INTO events (status, owner_id, created_at) VALUES (572271, 'text 543481', now() + interval '13 days'), (828496, E'it''s\n', NULL)
  ON CONFLICT (id) DO UPDATE SET amount = EXCLUDED.id RETURNING id;
UPDATE sessions SET status = owner_id + 95, region = CASE WHEN score > 637 THEN 'big' ELSE 'small' END WHERE id = $1 AND id <> ALL (ARRAY[7, 2, 0]);
WITH recent AS (SELECT * FROM invoices WHERE created_at > current_date - 11), ranked AS (
  SELECT *, row_number() OVER (PARTITION BY score ORDER BY owner_id DESC) AS rn FROM recent)
SELECT r.*, (SELECT max(amount) FROM customers x WHERE x.id = r.id) FROM ranked r WHERE rn <= 5
UNION ALL SELECT * FROM orders WHERE EXISTS (SELECT 1 FROM sessions WHERE score = $2);
DELETE FROM orders USING products WHERE orders.status = orders.id AND orders.id < now() - interval '1 year' RETURNING *;
SELECT a.name, b.status AS "B Col", count(*) FILTER (WHERE a.id > 846) AS n, coalesce(sum(a.region), 0)::numeric(12, 2)
  FROM products AS a LEFT JOIN customers b ON b.id = a.created_at AND b.status IS NOT NULL
  WHERE a.name BETWEEN 90 AND 705 AND a.score IN ('x', 'y', 'z') AND NOT a.owner_id LIKE 'p%'
  GROUP BY 1, 2 HAVING count(*) > 7 ORDER BY n DESC, 1 LIMIT 41 OFFSET 668;
INSERT INTO events (amount, owner_id, name) VALUES (507471, 'text 470944', now() + interval '6 days'), (958318, E'it''s\n', NULL)
  ON CONFLICT (id) DO UPDATE SET status = EXCLUDED.status RETURNING id;
UPDATE orders SET status = id + 54, amount = CASE WHEN created_at > 889 THEN 'big' ELSE 'small' END WHERE id = $1 AND score <> ALL (ARRAY[8, 0, 9]);
WITH recent AS (SELECT * FROM customers WHERE region > current_date - 53), ranked AS (
  SELECT *, row_number() OVER (PARTITION BY name ORDER BY created_at DESC) AS rn FROM recent)
SELECT r.*, (SELECT max(created_at) FROM customers x WHERE x.id = r.id) FROM ranked r WHERE rn <= 3
UNION ALL SELECT * FROM events WHERE EXISTS (SELECT 1 FROM sessions WHERE created_at = $2);
DELETE FROM sessions USING products WHERE orders.name = orders.id AND orders.region < now() - interval '1 year' RETURNING *;
SELECT a.owner_id, b.amount AS "B Col", count(*) FILTER (WHERE a.amount > 206) AS n, coalesce(sum(a.created_at), 0)::numeric(12, 2)
  FROM customers AS a LEFT JOIN sessions b ON b.id = a.score AND b.name IS NOT NULL
  WHERE a.region BETWEEN 67 AND 508 AND a.status IN ('x', 'y', 'z') AND NOT a.id LIKE 'p%'
  GROUP BY 1, 2 HAVING count(*) > 7 ORDER BY n DESC, 1 LIMIT 96 OFFSET 735;
INSERT INTO sessions (created_at, owner_id, amount) VALUES (226604, 'text 938158', now() + interval '15 days'), (34726, E'it''s\n', NULL)
  ON CONFLICT (id) DO UPDATE SET id = EXCLUDED.amount RETURNING id;
UPDATE products SET region = name + 63, amount = CASE WHEN region > 553 THEN 'big' ELSE 'small' END WHERE id = $1 AND amount <> ALL (ARRAY[0, 4, 2]);
WITH recent AS (SELECT * FROM customers WHERE region > current_date - 46), ranked AS (
  SELECT *, row_number() OVER (PARTITION BY created_at ORDER BY name DESC) AS rn FROM recent)
SELECT r.*, (SELECT max(id) FROM customers x WHERE x.id = r.id) FROM ranked r WHERE rn <= 5
UNION ALL SELECT * FROM orders WHERE EXISTS (SELECT 1 FROM events WHERE id = $2);
DELETE FROM products USING products WHERE orders.id = orders.id AND orders.id < now() - interval '1 year' RETURNING *;
SELECT a.created_at, b.owner_id AS "B Col", count(*) FILTER (WHERE a.id > 700) AS n, coalesce(sum(a.id), 0)::numeric(12, 2)
  FROM events AS a LEFT JOIN invoices b ON b.id = a.amount AND b.score IS NOT NULL
  WHERE a.created_at BETWEEN 97 AND 494 AND a.created_at IN ('x', 'y', 'z') AND NOT a.score LIKE 'p%'
  GROUP BY 1, 2 HAVING count(*) > 6 ORDER BY n DESC, 1 LIMIT 27 OFFSET 720;
INSERT INTO events (status, region, score) VALUES (483363, 'text 116283', now() + interval '1 days'), (648520, E'it''s\n', NULL)
  ON CONFLICT (id) DO UPDATE SET region = EXCLUDED.created_at RETURNING id;
UPDATE invoices SET status = amount + 4, id = CASE WHEN amount > 91 THEN 'big' ELSE 'small' END WHERE id = $1 AND status <> ALL (ARRAY[8, 2, 9]);
WITH recent AS (SELECT * FROM invoices WHERE status > current_date - 59), ranked AS (
  SELECT *, row_number() OVER (PARTITION BY region ORDER BY amount DESC) AS rn FROM recent)
SELECT r.*, (SELECT max(status) FROM products x WHERE x.id = r.id) FROM ranked r WHERE rn <= 4
UNION ALL SELECT * FROM invoices WHERE EXISTS (SELECT 1 FROM products WHERE status = $2);
DELETE FROM invoices USING products WHERE orders.id = orders.id AND orders.status < now() - interval '1 year' RETURNING *;
SELECT a.score, b.region AS "B Col", count(*) FILTER (WHERE a.name > 909) AS n, coalesce(sum(a.name), 0)::numeric(12, 2)
  FROM orders AS a LEFT JOIN customers b ON b.id = a.status AND b.amount IS NOT NULL
  WHERE a.owner_id BETWEEN 11 AND 819 AND a.created_at IN ('x', 'y', 'z') AND NOT a.name LIKE 'p%'
  GROUP BY 1, 2 HAVING count(*) > 5 ORDER BY n DESC, 1 LIMIT 23 OFFSET 657;
INSERT INTO events (amount, score, name) VALUES (968778, 'text 927165', now() + interval '13 days'), (964742, E'it''s\n', NULL)
  ON CONFLICT (id) DO UPDATE SET amount = EXCLUDED.name RETURNING id;
UPDATE products SET status = amount + 38, region = CASE WHEN score > 711 THEN 'big' ELSE 'small' END WHERE id = $1 AND region <> ALL (ARRAY[5, 9, 4]);
WITH recent AS (SELECT * FROM events WHERE created_at > current_date - 0), ranked AS (
  SELECT *, row_number() OVER (PARTITION BY created_at ORDER BY status DESC) AS rn FROM recent)
SELECT r.*, (SELECT max(created_at) FROM orders x WHERE x.id = r.id) FROM ranked r WHERE rn <= 3
UNION ALL SELECT * FROM invoices WHERE EXISTS (SELECT 1 FROM invoices WHERE amount = $2);
DELETE FROM invoices USING sessions WHERE orders.status = orders.id AND orders.name < now() - interval '1 year' RETURNING *;
SELECT a.name, b.id AS "B Col", count(*) FILTER (WHERE a.region > 967) AS n, coalesce(sum(a.name), 0)::numeric(12, 2)
  FROM customers AS a LEFT JOIN sessions b ON b.id = a.region AND b.amount IS NOT NULL
  WHERE a.created_at BETWEEN 80 AND 651 AND a.name IN ('x', 'y', 'z') AND NOT a.owner_id LIKE 'p%'
  GROUP BY 1, 2 HAVING count(*) > 1 ORDER BY n DESC, 1 LIMIT 24 OFFSET 101;
INSERT INTO orders (created_at, region, id) VALUES (649211, 'text 786930', now() + interval '17 days'), (206918, E'it''s\n', NULL)
  ON CONFLICT (id) DO UPDATE SET name = EXCLUDED.id RETURNING id;
UPDATE invoices SET score = score + 82, region = CASE WHEN score > 46 THEN 'big' ELSE 'small' END WHERE id = $1 AND status <> ALL (ARRAY[4, 8, 2]);
WITH recent AS (SELECT * FROM invoices WHERE name > current_date - 23), ranked AS (
  SELECT *, row_number() OVER (PARTITION BY status ORDER BY owner_id DESC) AS rn FROM recent)
SELECT r.*, (SELECT max(owner_id) FROM orders x WHERE x.id = r.id) FROM ranked r WHERE rn <= 0
UNION ALL SELECT * FROM customers WHERE EXISTS (SELECT 1 FROM invoices WHERE owner_id = $2);
DELETE FROM invoices USING sessions WHERE orders.status = orders.id AND orders.id < now() - interval '1 year' RETURNING *;
SELECT a.amount, b.status AS "B Col", count(*) FILTER (WHERE a.region > 401) AS n, coalesce(sum(a.created_at), 0)::numeric(12, 2)
  FROM events AS a LEFT JOIN products b ON b.id = a.id AND b.name IS NOT NULL
  WHERE a.name BETWEEN 40 AND 1060 AND a.owner_id IN ('x', 'y', 'z') AND NOT a.name LIKE 'p%'
  GROUP BY 1, 2 HAVING count(*) > 9 ORDER BY n DESC, 1 LIMIT 15 OFFSET 594;
INSERT INTO invoices (score, owner_id, score) VALUES (732587, 'text 948298', now() + interval '11 days'), (587136, E'it''s\n', NULL)
  ON CONFLICT (id) DO UPDATE SET name = EXCLUDED.name RETURNING id;
UPDATE invoices SET score = name + 47, owner_id = CASE WHEN score > 549 THEN 'big' ELSE 'small' END WHERE id = $1 AND region <> ALL (ARRAY[9, 0, 6]);
WITH recent AS (SELECT * FROM customers WHERE amount > current_date - 54), ranked AS (
  SELECT *, row_number() OVER (PARTITION BY created_at ORDER BY score DESC) AS rn FROM recent)
SELECT r.*, (SELECT max(id) FROM invoices x WHERE x.id = r.id) FROM ranked r WHERE rn <= 9
UNION ALL SELECT * FROM events WHERE EXISTS (SELECT 1 FROM products WHERE id = $2);
DELETE FROM invoices USING orders WHERE orders.name = orders.id AND orders.id < now() - interval '1 year' RETURNING *;
SELECT a.amount, b.region AS "B Col", count(*) FILTER (WHERE a.owner_id > 413) AS n, coalesce(sum(a.name), 0)::numeric(12, 2)
  FROM orders AS a LEFT JOIN products b ON b.id = a.id AND b.owner_id IS NOT NULL
  WHERE a.name BETWEEN 73 AND 759 AND a.owner_id IN ('x', 'y', 'z') AND NOT a.region LIKE 'p%'
  GROUP BY 1, 2 HAVING count(*) > 3 ORDER BY n DESC, 1 LIMIT 22 OFFSET 998;
INSERT INTO events (name, region, score) VALUES (316792, 'text 995824', now() + interval '19 days'), (735107, E'it''s\n', NULL)
  ON CONFLICT (id) DO UPDATE SET region = EXCLUDED.id RETURNING id;
UPDATE customers SET status = name + 56, score = CASE WHEN region > 756 THEN 'big' ELSE 'small' END WHERE id = $1 AND owner_id <> ALL (ARRAY[2, 3, 9]);
WITH recent AS (SELECT * FROM products WHERE created_at > current_date - 46), ranked AS (
  SELECT *, row_number() OVER (PARTITION BY owner_id ORDER BY name DESC) AS rn FROM recent)
SELECT r.*, (SELECT max(score) FROM customers x WHERE x.id = r.id) FROM ranked r WHERE rn <= 8
UNION ALL SELECT * FROM sessions WHERE EXISTS (SELECT 1 FROM invoices WHERE owner_id = $2);
DELETE FROM invoices USING orders WHERE orders.created_at = orders.id AND orders.created_at < now() - interval '1 year' RETURNING *;
SELECT a.status, b.owner_id AS "B Col", count(*) FILTER (WHERE a.status > 620) AS n, coalesce(sum(a.id), 0)::numeric(12, 2)
  FROM sessions AS a LEFT JOIN sessions b ON b.id = a.amount AND b.id IS NOT NULL
  WHERE a.amount BETWEEN 5 AND 119 AND a.owner_id IN ('x', 'y', 'z') AND NOT a.id LIKE 'p%'
  GROUP BY 1, 2 HAVING count(*) > 9 ORDER BY n DESC, 1 LIMIT 14 OFFSET 436;
INSERT INTO sessions (owner_id, owner_id, owner_id) VALUES (836531, 'text 594684', now() + interval '20 days'), (495742, E'it''s\n', NULL)
  ON CONFLICT (id) DO UPDATE SET name = EXCLUDED.region RETURNING id;
UPDATE orders SET owner_id = name + 97, status = CASE WHEN amount > 965 THEN 'big' ELSE 'small' END WHERE id = $1 AND owner_id <> ALL (ARRAY[5, 2, 3]);
WITH recent AS (SELECT * FROM sessions WHERE created_at > current_date - 25), ranked AS (
  SELECT *, row_number() OVER (PARTITION BY region ORDER BY region DESC) AS rn FROM recent)
SELECT r.*, (SELECT max(created_at) FROM sessions x WHERE x.id = r.id) FROM ranked r WHERE rn <= 5
UNION ALL SELECT * FROM events WHERE EXISTS (SELECT 1 FROM orders WHERE name = $2);
DELETE FROM products USING events WHERE orders.owner_id = orders.id AND orders.created_at < now() - interval '1 year' RETURNING *;
SELECT a.amount, b.name AS "B Col", count(*) FILTER (WHERE a.created_at > 162) AS n, coalesce(sum(a.score), 0)::numeric(12, 2)
  FROM sessions AS a LEFT JOIN invoices b ON b.id = a.amount AND b.id IS NOT NULL
  WHERE a.status BETWEEN 85 AND 251 AND a.name IN ('x', 'y', 'z') AND NOT a.owner_id LIKE 'p%'
  GROUP BY 1, 2 HAVING count(*) > 6 ORDER BY n DESC, 1 LIMIT 45 OFFSET 159;
INSERT INTO products (owner_id, region, owner_id) VALUES (801539, 'text 437625', now() + interval '13 days'), (286981, E'it''s\n', NULL)
  ON CONFLICT (id) DO UPDATE SET score = EXCLUDED.id RETURNING id;
UPDATE events SET status = owner_id + 76, created_at = CASE WHEN region > 395 THEN 'big' ELSE 'small' END WHERE id = $1 AND owner_id <> ALL (ARRAY[9, 5, 6]);
WITH recent AS (SELECT * FROM events WHERE region > current_date - 71), ranked AS (
  SELECT *, row_number() OVER (PARTITION BY owner_id ORDER BY amount DESC) AS rn FROM recent)
SELECT r.*, (SELECT max(owner_id) FROM customers x WHERE x.id = r.id) FROM ranked r WHERE rn <= 9
UNION ALL SELECT * FROM products WHERE EXISTS (SELECT 1 FROM products WHERE amount = $2);
DELETE FROM sessions USING products WHERE orders.status = orders.id AND orders.id < now() - interval '1 year' RETURNING *;
SELECT a.score, b.score AS "B Col", count(*) FILTER (WHERE a.region > 923) AS n, coalesce(sum(a.name), 0)::numeric(12, 2)
  FROM sessions AS a LEFT JOIN customers b ON b.id = a.name AND b.score IS NOT NULL
  WHERE a.amount BETWEEN 58 AND 1053 AND a.id IN ('x', 'y', 'z') AND NOT a.owner_id LIKE 'p%'
  GROUP BY 1, 2 HAVING count(*) > 2 ORDER BY n DESC, 1 LIMIT 18 OFFSET 655;
INSERT INTO customers (name, created_at, id) VALUES (981262, 'text 642615', now() + interval '26 days'), (894041, E'it''s\n', NULL)
  ON CONFLICT (id) DO UPDATE SET name = EXCLUDED.amount RETURNING id;
UPDATE customers SET name = region + 23, score = CASE WHEN id > 66 THEN 'big' ELSE 'small' END WHERE id = $1 AND status <> ALL (ARRAY[1, 1, 2]);
WITH recent AS (SELECT * FROM invoices WHERE status > current_date - 15), ranked AS (
  SELECT *, row_number() OVER (PARTITION BY amount ORDER BY score DESC) AS rn FROM recent)
SELECT r.*, (SELECT max(score) FROM invoices x WHERE x.id = r.id) FROM ranked r WHERE rn <= 9
UNION ALL SELECT * FROM sessions WHERE EXISTS (SELECT 1 FROM products WHERE score = $2);
DELETE FROM products USING customers WHERE orders.status = orders.id AND orders.region < now() - interval '1 year' RETURNING *;
SELECT a.owner_id, b.owner_id AS "B Col", count(*) FILTER (WHERE a.region > 376) AS n, coalesce(sum(a.owner_id), 0)::numeric(12, 2)
  FROM events AS a LEFT JOIN orders b ON b.id = a.region AND b.region IS NOT NULL
  WHERE a.owner_id BETWEEN 49 AND 326 AND a.region IN ('x', 'y', 'z') AND NOT a.status LIKE 'p%'
  GROUP BY 1, 2 HAVING count(*) > 5 ORDER BY n DESC, 1 LIMIT 84 OFFSET 349;
INSERT INTO invoices (score, amount, owner_id) VALUES (238746, 'text 913466', now() + interval '1 days'), (792045, E'it''s\n', NULL)
  ON CONFLICT (id) DO UPDATE SET owner_id = EXCLUDED.name RETURNING id;
UPDATE orders SET status = score + 17, status = CASE WHEN status > 882 THEN 'big' ELSE 'small' END WHERE id = $1 AND amount <> ALL (ARRAY[8, 6, 8]);
WITH recent AS (SELECT * FROM products WHERE amount > current_date - 32), ranked AS (
  SELECT *, row_number() OVER (PARTITION BY status ORDER BY status DESC) AS rn FROM recent)
SELECT r.*, (SELECT max(created_at) FROM customers x WHERE x.id = r.id) FROM ranked r WHERE rn <= 7
UNION ALL SELECT * FROM products WHERE EXISTS (SELECT 1 FROM sessions WHERE amount = $2);
DELETE FROM orders USING products WHERE orders.owner_id = orders.id AND orders.amount < now() - interval '1 year' RETURNING *;
SELECT a.name, b.id AS "B Col", count(*) FILTER (WHERE a.score > 316) AS n, coalesce(sum(a.owner_id), 0)::numeric(12, 2)
  FROM products AS a LEFT JOIN sessions b ON b.id = a.region AND b.owner_id IS NOT NULL
  WHERE a.id BETWEEN 8 AND 366 AND a.status IN ('x', 'y', 'z') AND NOT a.id LIKE 'p%'
  GROUP BY 1, 2 HAVING count(*) > 5 ORDER BY n DESC, 1 LIMIT 1 OFFSET 900;
INSERT INTO events (score, created_at, created_at) VALUES (12500, 'text 60979', now() + interval '9 days'), (805049, E'it''s\n', NULL)
  ON CONFLICT (id) DO UPDATE SET score = EXCLUDED.created_at RETURNING id;
UPDATE events SET region = status + 17, owner_id = CASE WHEN status > 366 THEN 'big' ELSE 'small' END WHERE id = $1 AND score <> ALL (ARRAY[8, 0, 8]);
WITH recent AS (SELECT * FROM products WHERE created_at > current_date - 17), ranked AS (
  SELECT *, row_number() OVER (PARTITION BY region ORDER BY created_at DESC) AS rn FROM recent)
SELECT r.*, (SELECT max(amount) FROM events x WHERE x.id = r.id) FROM ranked r WHERE rn <= 5
UNION ALL SELECT * FROM orders WHERE EXISTS (SELECT 1 FROM events WHERE score = $2);
DELETE FROM orders USING invoices WHERE orders.created_at = orders.id AND orders.amount < now() - interval '1 year' RETURNING *;
SELECT a.score, b.region AS "B Col", count(*) FILTER (WHERE a.name > 370) AS n, coalesce(sum(a.region), 0)::numeric(12, 2)
  FROM sessions AS a LEFT JOIN products b ON b.id = a.score AND b.score IS NOT NULL
  WHERE a.status BETWEEN 13 AND 964 AND a.owner_id IN ('x', 'y', 'z') AND NOT a.created_at LIKE 'p%'
  GROUP BY 1, 2 HAVING count(*) > 5 ORDER BY n DESC, 1 LIMIT 80 OFFSET 471;
INSERT INTO sessions (created_at, status, score) VALUES (319591, 'text 391423', now() + interval '0 days'), (942893, E'it''s\n', NULL)
  ON CONFLICT (id) DO UPDATE SET score = EXCLUDED.status RETURNING id;
UPDATE invoices SET name = name + 17, amount = CASE WHEN id > 789 THEN 'big' ELSE 'small' END WHERE id = $1 AND score <> ALL (ARRAY[3, 8, 2]);
WITH recent AS (SELECT * FROM customers WHERE name > current_date - 22), ranked AS (
  SELECT *, row_number() OVER (PARTITION BY owner_id ORDER BY amount DESC) AS rn FROM recent)
SELECT r.*, (SELECT max(name) FROM invoices x WHERE x.id = r.id) FROM ranked r WHERE rn <= 2
UNION ALL SELECT * FROM events WHERE EXISTS (SELECT 1 FROM orders WHERE name = $2);
DELETE FROM invoices USING customers WHERE orders.created_at = orders.id AND orders.owner_id < now() - interval '1 year' RETURNING *;
SELECT a.status, b.owner_id AS "B Col", count(*) FILTER (WHERE a.amount > 227) AS n, coalesce(sum(a.amount), 0)::numeric(12, 2)
  FROM events AS a LEFT JOIN events b ON b.id = a.id AND b.id IS NOT NULL
  WHERE a.amount BETWEEN 85 AND 399 AND a.name IN ('x', 'y', 'z') AND NOT a.name LIKE 'p%'
  GROUP BY 1, 2 HAVING count(*) > 0 ORDER BY n DESC, 1 LIMIT 25 OFFSET 809;
INSERT INTO events (region, id, region) VALUES (985768, 'text 182729', now() + interval '24 days'), (993297, E'it''s\n', NULL)
  ON CONFLICT (id) DO UPDATE SET id = EXCLUDED.name RETURNING id;
UPDATE orders SET score = score + 30, created_at = CASE WHEN created_at > 93 THEN 'big' ELSE 'small' END WHERE id = $1 AND status <> ALL (ARRAY[6, 7, 5]);
WITH recent AS (SELECT * FROM products WHERE created_at > current_date - 31), ranked AS (
  SELECT *, row_number() OVER (PARTITION BY id ORDER BY created_at DESC) AS rn FROM recent)
SELECT r.*, (SELECT max(name) FROM products x WHERE x.id = r.id) FROM ranked r WHERE rn <= 2
UNION ALL SELECT * FROM invoices WHERE EXISTS (SELECT 1 FROM sessions WHERE score = $2);
DELETE FROM customers USING orders WHERE orders.region = orders.id AND orders.amount < now() - interval '1 year' RETURNING *;
SELECT a.status, b.status AS "B Col", count(*) FILTER (WHERE a.region > 583) AS n, coalesce(sum(a.status), 0)::numeric(12, 2)
  FROM products AS a LEFT JOIN customers b ON b.id = a.id AND b.amount IS NOT NULL
  WHERE a.name BETWEEN 58 AND 289 AND a.created_at IN ('x', 'y', 'z') AND NOT a.name LIKE 'p%'
  GROUP BY 1, 2 HAVING count(*) > 7 ORDER BY n DESC, 1 LIMIT 96 OFFSET 430;
INSERT INTO invoices (region, id, created_at) VALUES (717851, 'text 378662', now() + interval '16 days'), (917572, E'it''s\n', NULL)
  ON CONFLICT (id) DO UPDATE SET owner_id = EXCLUDED.id RETURNING id;
UPDATE invoices SET score = id + 30, status = CASE WHEN amount > 701 THEN 'big' ELSE 'small' END WHERE id = $1 AND name <> ALL (ARRAY[0, 9, 2]);
WITH recent AS (SELECT * FROM products WHERE id > current_date - 11), ranked AS (
  SELECT *, row_number() OVER (PARTITION BY name ORDER BY owner_id DESC) AS rn FROM recent)
SELECT r.*, (SELECT max(region) FROM sessions x WHERE x.id = r.id) FROM ranked r WHERE rn <= 7
UNION ALL SELECT * FROM products WHERE EXISTS (SELECT 1 FROM sessions WHERE amount = $2);
DELETE FROM sessions USING customers WHERE orders.owner_id = orders.id AND orders.amount < now() - interval '1 year' RETURNING *;
SELECT a.score, b.amount AS "B Col", count(*) FILTER (WHERE a.score > 593) AS n, coalesce(sum(a.owner_id), 0)::numeric(12, 2)
  FROM customers AS a LEFT JOIN events b ON b.id = a.score AND b.name IS NOT NULL
  WHERE a.id BETWEEN 83 AND 909 AND a.score IN ('x', 'y', 'z') AND NOT a.amount LIKE 'p%'
  GROUP BY 1, 2 HAVING count(*) > 3 ORDER BY n DESC, 1 LIMIT 4 OFFSET 12;
INSERT INTO orders (amount, id, created_at) VALUES (569640, 'text 809127', now() + interval '7 days'), (543302, E'it''s\n', NULL)
  ON CONFLICT (id) DO UPDATE SET score = EXCLUDED.amount RETURNING id;
UPDATE events SET region = created_at + 4, amount = CASE WHEN name > 547 THEN 'big' ELSE 'small' END WHERE id = $1 AND amount <> ALL (ARRAY[1, 1, 1]);
WITH recent AS (SELECT * FROM events WHERE owner_id > current_date - 45), ranked AS (
  SELECT *, row_number() OVER (PARTITION BY status ORDER BY score DESC) AS rn FROM recent)
SELECT r.*, (SELECT max(amount) FROM events x WHERE x.id = r.id) FROM ranked r WHERE rn <= 4
UNION ALL SELECT * FROM invoices WHERE EXISTS (SELECT 1 FROM orders WHERE amount = $2);
DELETE FROM sessions USING events WHERE orders.status = orders.id AND orders.region < now() - interval '1 year' RETURNING *;
SELECT a.region, b.score AS "B Col", count(*) FILTER (WHERE a.amount > 390) AS n, coalesce(sum(a.amount), 0)::numeric(12, 2)
  FROM products AS a LEFT JOIN products b ON b.id = a.name AND b.region IS NOT NULL
  WHERE a.owner_id BETWEEN 0 AND 362 AND a.name IN ('x', 'y', 'z') AND NOT a.id LIKE 'p%'
  GROUP BY 1, 2 HAVING count(*) > 3 ORDER BY n DESC, 1 LIMIT 51 OFFSET 67;
INSERT INTO orders (created_at, id, name) VALUES (748835, 'text 762223', now() + interval '23 days'), (98157, E'it''s\n', NULL)
  ON CONFLICT (id) DO UPDATE SET name = EXCLUDED.name RETURNING id;
UPDATE customers SET amount = status + 26, region = CASE WHEN name > 549 THEN 'big' ELSE 'small' END WHERE id = $1 AND name <> ALL (ARRAY[5, 1, 2]);
WITH recent AS (SELECT * FROM orders WHERE amount > current_date - 28), ranked AS (
  SELECT *, row_number() OVER (PARTITION BY score ORDER BY amount DESC) AS rn FROM recent)
SELECT r.*, (SELECT max(name) FROM orders x WHERE x.id = r.id) FROM ranked r WHERE rn <= 1
UNION ALL SELECT * FROM customers WHERE EXISTS (SELECT 1 FROM customers WHERE status = $2);
DELETE FROM events USING products WHERE orders.name = orders.id AND orders.created_at < now() - interval '1 year' RETURNING *;
SELECT a.amount, b.score AS "B Col", count(*) FILTER (WHERE a.owner_id > 487) AS n, coalesce(sum(a.score), 0)::numeric(12, 2)
  FROM invoices AS a LEFT JOIN products b ON b.id = a.region AND b.amount IS NOT NULL
  WHERE a.status BETWEEN 18 AND 386 AND a.amount IN ('x', 'y', 'z') AND NOT a.score LIKE 'p%'
  GROUP BY 1, 2 HAVING count(*) > 5 ORDER BY n DESC, 1 LIMIT 32 OFFSET 437;
INSERT INTO orders (id, status, status) VALUES (574114, 'text 303704', now() + interval '1 days'), (671452, E'it''s\n', NULL)
  ON CONFLICT (id) DO UPDATE SET owner_id = EXCLUDED.owner_id RETURNING id;
UPDATE orders SET created_at = region + 2, created_at = CASE WHEN amount > 95 THEN 'big' ELSE 'small' END WHERE id = $1 AND amount <> ALL (ARRAY[2, 0, 4]);
WITH recent AS (SELECT * FROM orders WHERE region > current_date - 44), ranked AS (
  SELECT *, row_number() OVER (PARTITION BY region ORDER BY amount DESC) AS rn FROM recent)
SELECT r.*, (SELECT max(owner_id) FROM invoices x WHERE x.id = r.id) FROM ranked r WHERE rn <= 3
UNION ALL SELECT * FROM sessions WHERE EXISTS (SELECT 1 FROM customers WHERE score = $2);
DELETE FROM customers USING invoices WHERE orders.amount = orders.id AND orders.owner_id < now() - interval '1 year' RETURNING *;
SELECT a.name, b.status AS "B Col", count(*) FILTER (WHERE a.created_at > 273) AS n, coalesce(sum(a.owner_id), 0)::numeric(12, 2)
  FROM products AS a LEFT JOIN invoices b ON b.id = a.status AND b.score IS NOT NULL
  WHERE a.id BETWEEN 63 AND 672 AND a.created_at IN ('x', 'y', 'z') AND NOT a.region LIKE 'p%'
  GROUP BY 1, 2 HAVING count(*) > 3 ORDER BY n DESC, 1 LIMIT 59 OFFSET 782;
INSERT INTO customers (name, owner_id, owner_id) VALUES (98417, 'text 757257', now() + interval '19 days'), (885230, E'it''s\n', NULL)
  ON CONFLICT (id) DO UPDATE SET created_at = EXCLUDED.status RETURNING id;
UPDATE orders SET amount = score + 73, owner_id = CASE WHEN amount > 327 THEN 'big' ELSE 'small' END WHERE id = $1 AND status <> ALL (ARRAY[8, 0, 9]);
WITH recent AS (SELECT * FROM orders WHERE created_at > current_date - 82), ranked AS (
  SELECT *, row_number() OVER (PARTITION BY name ORDER BY score DESC) AS rn FROM recent)
SELECT r.*, (SELECT max(amount) FROM invoices x WHERE x.id = r.id) FROM ranked r WHERE rn <= 2
UNION ALL SELECT * FROM orders WHERE EXISTS (SELECT 1 FROM invoices WHERE id = $2);
DELETE FROM orders USING customers WHERE orders.created_at = orders.id AND orders.owner_id < now() - interval '1 year' RETURNING *;
SELECT a.name, b.score AS "B Col", count(*) FILTER (WHERE a.name > 510) AS n, coalesce(sum(a.owner_id), 0)::numeric(12, 2)
  FROM sessions AS a LEFT JOIN events b ON b.id = a.status AND b.score IS NOT NULL
  WHERE a.score BETWEEN 98 AND 486 AND a.region IN ('x', 'y', 'z') AND NOT a.status LIKE 'p%'
  GROUP BY 1, 2 HAVING count(*) > 6 ORDER BY n DESC, 1 LIMIT 38 OFFSET 508;
INSERT INTO invoices (status, owner_id, name) VALUES (263644, 'text 299879', now() + interval '2 days'), (177182, E'it''s\n', NULL)
  ON CONFLICT (id) DO UPDATE SET score = EXCLUDED.created_at RETURNING id;
UPDATE products SET status = status + 29, status = CASE WHEN name > 369 THEN 'big' ELSE 'small' END WHERE id = $1 AND amount <> ALL (ARRAY[5, 9, 5]);
WITH recent AS (SELECT * FROM invoices WHERE amount > current_date - 17), ranked AS (
  SELECT *, row_number() OVER (PARTITION BY amount ORDER BY id DESC) AS rn FROM recent)
SELECT r.*, (SELECT max(id) FROM events x WHERE x.id = r.id) FROM ranked r WHERE rn <= 2
UNION ALL SELECT * FROM invoices WHERE EXISTS (SELECT 1 FROM invoices WHERE owner_id = $2);
DELETE FROM sessions USING products WHERE orders.score = orders.id AND orders.owner_id < now() - interval '1 year' RETURNING *;
SELECT a.created_at, b.amount AS "B Col", count(*) FILTER (WHERE a.region > 754) AS n, coalesce(sum(a.owner_id), 0)::numeric(12, 2)
  FROM orders AS a LEFT JOIN invoices b ON b.id = a.amount AND b.region IS NOT NULL
  WHERE a.id BETWEEN 33 AND 975 AND a.amount IN ('x', 'y', 'z') AND NOT a.name LIKE 'p%'
  GROUP BY 1, 2 HAVING count(*) > 0 ORDER BY n DESC, 1 LIMIT 58 OFFSET 540;
INSERT INTO orders (status, region, created_at) VALUES (332029, 'text 354667', now() + interval '17 days'), (534211, E'it''s\n', NULL)
  ON CONFLICT (id) DO UPDATE SET id = EXCLUDED.name RETURNING id;
UPDATE products SET created_at = score + 69, score = CASE WHEN score > 443 THEN 'big' ELSE 'small' END WHERE id = $1 AND region <> ALL (ARRAY[6, 8, 5]);
WITH recent AS (SELECT * FROM invoices WHERE id > current_date - 30), ranked AS (
  SELECT *, row_number() OVER (PARTITION BY name ORDER BY score DESC) AS rn FROM recent)
SELECT r.*, (SELECT max(region) FROM products x WHERE x.id = r.id) FROM ranked r WHERE rn <= 7
UNION ALL SELECT * FROM customers WHERE EXISTS (SELECT 1 FROM sessions WHERE status = $2);
DELETE FROM products USING orders WHERE orders.owner_id = orders.id AND orders.owner_id < now() - interval '1 year' RETURNING *;
SELECT a.status, b.id AS "B Col", count(*) FILTER (WHERE a.created_at > 64) AS n, coalesce(sum(a.owner_id), 0)::numeric(12, 2)
  FROM events AS a LEFT JOIN customers b ON b.id = a.score AND b.amount IS NOT NULL
  WHERE a.region BETWEEN 35 AND 938 AND a.score IN ('x', 'y', 'z') AND NOT a.created_at LIKE 'p%'
  GROUP BY 1, 2 HAVING count(*) > 7 ORDER BY n DESC, 1 LIMIT 12 OFFSET 690;
INSERT INTO orders (owner_id, created_at, id) VALUES (999978, 'text 382315', now() + interval '24 days'), (350115, E'it''s\n', NULL)
  ON CONFLICT (id) DO UPDATE SET region = EXCLUDED.amount RETURNING id;
UPDATE events SET region = id + 84, id = CASE WHEN score > 426 THEN 'big' ELSE 'small' END WHERE id = $1 AND region <> ALL (ARRAY[6, 9, 3]);
WITH recent AS (SELECT * FROM orders WHERE owner_id > current_date - 44), ranked AS (
  SELECT *, row_number() OVER (PARTITION BY id ORDER BY score DESC) AS rn FROM recent)
SELECT r.*, (SELECT max(score) FROM invoices x WHERE x.id = r.id) FROM ranked r WHERE rn <= 5
UNION ALL SELECT * FROM sessions WHERE EXISTS (SELECT 1 FROM orders WHERE amount = $2);
DELETE FROM events USING invoices WHERE orders.region = orders.id AND orders.region < now() - interval '1 year' RETURNING *;
SELECT a.score, b.score AS "B Col", count(*) FILTER (WHERE a.created_at > 574) AS n, coalesce(sum(a.name), 0)::numeric(12, 2)
  FROM invoices AS a LEFT JOIN sessions b ON b.id = a.name AND b.region IS NOT NULL
  WHERE a.amount BETWEEN 28 AND 943 AND a.amount IN ('x', 'y', 'z') AND NOT a.status LIKE 'p%'
  GROUP BY 1, 2 HAVING count(*) > 6 ORDER BY n DESC, 1 LIMIT 11 OFFSET 209;
INSERT INTO customers (amount, created_at, name) VALUES (986609, 'text 773588', now() + interval '27 days'), (745439, E'it''s\n', NULL)
  ON CONFLICT (id) DO UPDATE SET amount = EXCLUDED.amount RETURNING id;
UPDATE customers SET status = region + 75, name = CASE WHEN region > 256 THEN 'big' ELSE 'small' END WHERE id = $1 AND owner_id <> ALL (ARRAY[1, 1, 9]);
WITH recent AS (SELECT * FROM orders WHERE name > current_date - 86), ranked AS (
  SELECT *, row_number() OVER (PARTITION BY amount ORDER BY created_at DESC) AS rn FROM recent)
SELECT r.*, (SELECT max(amount) FROM sessions x WHERE x.id = r.id) FROM ranked r WHERE rn <= 5
UNION ALL SELECT * FROM sessions WHERE EXISTS (SELECT 1 FROM products WHERE name = $2);
DELETE FROM events USING orders WHERE orders.status = orders.id AND orders.owner_id < now() - interval '1 year' RETURNING *;
SELECT a.name, b.status AS "B Col", count(*) FILTER (WHERE a.status > 690) AS n, coalesce(sum(a.id), 0)::numeric(12, 2)
  FROM products AS a LEFT JOIN orders b ON b.id = a.status AND b.owner_id IS NOT NULL
  WHERE a.owner_id BETWEEN 86 AND 880 AND a.amount IN ('x', 'y', 'z') AND NOT a.amount LIKE 'p%'
  GROUP BY 1, 2 HAVING count(*) > 7 ORDER BY n DESC, 1 LIMIT 39 OFFSET 766;
INSERT INTO customers (name, score, region) VALUES (974589, 'text 471199', now() + interval '27 days'), (95867, E'it''s\n', NULL)
  ON CONFLICT (id) DO UPDATE SET owner_id = EXCLUDED.score RETURNING id;
UPDATE sessions SET owner_id = amount + 16, created_at = CASE WHEN region > 962 THEN 'big' ELSE 'small' END WHERE id = $1 AND score <> ALL (ARRAY[9, 9, 6]);
WITH recent AS (SELECT * FROM products WHERE name > current_date - 43), ranked AS (
  SELECT *, row_number() OVER (PARTITION BY created_at ORDER BY name DESC) AS rn FROM recent)
SELECT r.*, (SELECT max(created_at) FROM sessions x WHERE x.id = r.id) FROM ranked r WHERE rn <= 4
UNION ALL SELECT * FROM customers WHERE EXISTS (SELECT 1 FROM sessions WHERE owner_id = $2);
DELETE FROM products USING events WHERE orders.id = orders.id AND orders.name < now() - interval '1 year' RETURNING *;
SELECT a.region, b.id AS "B Col", count(*) FILTER (WHERE a.region > 647) AS n, coalesce(sum(a.owner_id), 0)::numeric(12, 2)
  FROM sessions AS a LEFT JOIN orders b ON b.id = a.score AND b.owner_id IS NOT NULL
  WHERE a.created_at BETWEEN 79 AND 963 AND a.region IN ('x', 'y', 'z') AND NOT a.status LIKE 'p%'
  GROUP BY 1, 2 HAVING count(*) > 3 ORDER BY n DESC, 1 LIMIT 23 OFFSET 906;
INSERT INTO orders (created_at, region, owner_id) VALUES (80431, 'text 318574', now() + interval '18 days'), (468312, E'it''s\n', NULL)
  ON CONFLICT (id) DO UPDATE SET created_at = EXCLUDED.status RETURNING id;
UPDATE customers SET region = name + 40, owner_id = CASE WHEN region > 647 THEN 'big' ELSE 'small' END WHERE id = $1 AND score <> ALL (ARRAY[2, 3, 0]);
WITH recent AS (SELECT * FROM events WHERE score > current_date - 69), ranked AS (
  SELECT *, row_number() OVER (PARTITION BY created_at ORDER BY name DESC) AS rn FROM recent)
SELECT r.*, (SELECT max(region) FROM orders x WHERE x.id = r.id) FROM ranked r WHERE rn <= 6
UNION ALL SELECT * FROM products WHERE EXISTS (SELECT 1 FROM events WHERE amount = $2);
DELETE FROM events USING events WHERE orders.score = orders.id AND orders.created_at < now() - interval '1 year' RETURNING *;
SELECT a.created_at, b.amount AS "B Col", count(*) FILTER (WHERE a.owner_id > 323) AS n, coalesce(sum(a.created_at), 0)::numeric(12, 2)
  FROM customers AS a LEFT JOIN orders b ON b.id = a.region AND b.created_at IS NOT NULL
  WHERE a.status BETWEEN 13 AND 884 AND a.owner_id IN ('x', 'y', 'z') AND NOT a.created_at LIKE 'p%'
  GROUP BY 1, 2 HAVING count(*) > 7 ORDER BY n DESC, 1 LIMIT 6 OFFSET 245;
INSERT INTO sessions (created_at, id, id) VALUES (724227, 'text 104297', now() + interval '5 days'), (850491, E'it''s\n', NULL)
  ON CONFLICT (id) DO UPDATE SET status = EXCLUDED.id RETURNING id;
UPDATE sessions SET amount = name + 61, created_at = CASE WHEN status > 818 THEN 'big' ELSE 'small' END WHERE id = $1 AND status <> ALL (ARRAY[1, 4, 0]);
WITH recent AS (SELECT * FROM orders WHERE owner_id > current_date - 58), ranked AS (
  SELECT *, row_number() OVER (PARTITION BY name ORDER BY status DESC) AS rn FROM recent)
SELECT r.*, (SELECT max(owner_id) FROM invoices x WHERE x.id = r.id) FROM ranked r WHERE rn <= 7
UNION ALL SELECT * FROM events WHERE EXISTS (SELECT 1 FROM products WHERE created_at = $2);
DELETE FROM invoices USING customers WHERE orders.amount = orders.id AND orders.created_at < now() - interval '1 year' RETURNING *;
SELECT a.region, b.status AS "B Col", count(*) FILTER (WHERE a.id > 202) AS n, coalesce(sum(a.score), 0)::numeric(12, 2)
  FROM invoices AS a LEFT JOIN products b ON b.id = a.status AND b.owner_id IS NOT NULL
  WHERE a.status BETWEEN 89 AND 973 AND a.amount IN ('x', 'y', 'z') AND NOT a.name LIKE 'p%'
  GROUP BY 1, 2 HAVING count(*) > 0 ORDER BY n DESC, 1 LIMIT 6 OFFSET 47;
INSERT INTO products (score, created_at, name) VALUES (389160, 'text 181466', now() + interval '10 days'), (223681, E'it''s\n', NULL)
  ON CONFLICT (id) DO UPDATE SET amount = EXCLUDED.amount RETURNING id;
UPDATE invoices SET created_at = created_at + 69, created_at = CASE WHEN status > 874 THEN 'big' ELSE 'small' END WHERE id = $1 AND status <> ALL (ARRAY[7, 1, 6]);
WITH recent AS (SELECT * FROM customers WHERE id > current_date - 42), ranked AS (
  SELECT *, row_number() OVER (PARTITION BY status ORDER BY region DESC) AS rn FROM recent)
SELECT r.*, (SELECT max(amount) FROM invoices x WHERE x.id = r.id) FROM ranked r WHERE rn <= 8
UNION ALL SELECT * FROM customers WHERE EXISTS (SELECT 1 FROM orders WHERE owner_id = $2);
DELETE FROM products USING orders WHERE orders.name = orders.id AND orders.owner_id < now() - interval '1 year' RETURNING *;
SELECT a.owner_id, b.score AS "B Col", count(*) FILTER (WHERE a.id > 17) AS n, coalesce(sum(a.created_at), 0)::numeric(12, 2)
  FROM invoices AS a LEFT JOIN events b ON b.id = a.region AND b.name IS NOT NULL
  WHERE a.region BETWEEN 70 AND 134 AND a.id IN ('x', 'y', 'z') AND NOT a.region LIKE 'p%'
  GROUP BY 1, 2 HAVING count(*) > 4 ORDER BY n DESC, 1 LIMIT 51 OFFSET 805;
INSERT INTO products (id, score, name) VALUES (325154, 'text 893494', now() + interval '20 days'), (699969, E'it''s\n', NULL)
  ON CONFLICT (id) DO UPDATE SET created_at = EXCLUDED.name RETURNING id;
UPDATE customers SET created_at = status + 11, region = CASE WHEN id > 955 THEN 'big' ELSE 'small' END WHERE id = $1 AND status <> ALL (ARRAY[3, 6, 3]);
WITH recent AS (SELECT * FROM products WHERE score > current_date - 81), ranked AS (
  SELECT *, row_number() OVER (PARTITION BY amount ORDER BY region DESC) AS rn FROM recent)
SELECT r.*, (SELECT max(region) FROM orders x WHERE x.id = r.id) FROM ranked r WHERE rn <= 5
UNION ALL SELECT * FROM orders WHERE EXISTS (SELECT 1 FROM sessions WHERE region = $2);
DELETE FROM orders USING invoices WHERE orders.amount = orders.id AND orders.score < now() - interval '1 year' RETURNING *;
SELECT a.amount, b.region AS "B Col", count(*) FILTER (WHERE a.owner_id > 357) AS n, coalesce(sum(a.score), 0)::numeric(12, 2)
  FROM products AS a LEFT JOIN customers b ON b.id = a.id AND b.id IS NOT NULL
  WHERE a.region BETWEEN 94 AND 582 AND a.owner_id IN ('x', 'y', 'z') AND NOT a.score LIKE 'p%'
  GROUP BY 1, 2 HAVING count(*) > 3 ORDER BY n DESC, 1 LIMIT 7 OFFSET 380;
INSERT INTO sessions (score, score, name) VALUES (276563, 'text 874515', now() + interval '0 days'), (34612, E'it''s\n', NULL)
  ON CONFLICT (id) DO UPDATE SET created_at = EXCLUDED.status RETURNING id;
UPDATE orders SET status = id + 30, score = CASE WHEN score > 812 THEN 'big' ELSE 'small' END WHERE id = $1 AND created_at <> ALL (ARRAY[6, 3, 8]);
WITH recent AS (SELECT * FROM products WHERE created_at > current_date - 58), ranked AS (
  SELECT *, row_number() OVER (PARTITION BY status ORDER BY name DESC) AS rn FROM recent)
SELECT r.*, (SELECT max(score) FROM customers x WHERE x.id = r.id) FROM ranked r WHERE rn <= 4
UNION ALL SELECT * FROM products WHERE EXISTS (SELECT 1 FROM products WHERE status = $2);
DELETE FROM orders USING customers WHERE orders.owner_id = orders.id AND orders.name < now() - interval '1 year' RETURNING *;
SELECT a.name, b.score AS "B Col", count(*) FILTER (WHERE a.region > 105) AS n, coalesce(sum(a.region), 0)::numeric(12, 2)
  FROM orders AS a LEFT JOIN orders b ON b.id = a.id AND b.score IS NOT NULL
  WHERE a.amount BETWEEN 58 AND 623 AND a.score IN ('x', 'y', 'z') AND NOT a.status LIKE 'p%'
  GROUP BY 1, 2 HAVING count(*) > 3 ORDER BY n DESC, 1 LIMIT 23 OFFSET 506;
INSERT INTO invoices (created_at, id, id) VALUES (66937, 'text 78471', now() + interval '5 days'), (954850, E'it''s\n', NULL)
  ON CONFLICT (id) DO UPDATE SET owner_id = EXCLUDED.created_at RETURNING id;
UPDATE products SET id = owner_id + 13, owner_id = CASE WHEN name > 780 THEN 'big' ELSE 'small' END WHERE id = $1 AND id <> ALL (ARRAY[0, 4, 5]);
WITH recent AS (SELECT * FROM sessions WHERE owner_id > current_date - 41), ranked AS (
  SELECT *, row_number() OVER (PARTITION BY id ORDER BY region DESC) AS rn FROM recent)
SELECT r.*, (SELECT max(created_at) FROM orders x WHERE x.id = r.id) FROM ranked r WHERE rn <= 3
UNION ALL SELECT * FROM events WHERE EXISTS (SELECT 1 FROM sessions WHERE amount = $2);
DELETE FROM events USING orders WHERE orders.created_at = orders.id AND orders.status < now() - interval '1 year' RETURNING *;
SELECT a.region, b.status AS "B Col", count(*) FILTER (WHERE a.region > 981) AS n, coalesce(sum(a.id), 0)::numeric(12, 2)
  FROM sessions AS a LEFT JOIN customers b ON b.id = a.amount AND b.status IS NOT NULL
  WHERE a.owner_id BETWEEN 94 AND 364 AND a.status IN ('x', 'y', 'z') AND NOT a.owner_id LIKE 'p%'
  GROUP BY 1, 2 HAVING count(*) > 7 ORDER BY n DESC, 1 LIMIT 80 OFFSET 245;
INSERT INTO sessions (id, name, owner_id) VALUES (295619, 'text 163461', now() + interval '13 days'), (864, E'it''s\n', NULL)
  ON CONFLICT (id) DO UPDATE SET owner_id = EXCLUDED.created_at RETURNING id;
UPDATE products SET status = score + 3, name = CASE WHEN created_at > 830 THEN 'big' ELSE 'small' END WHERE id = $1 AND name <> ALL (ARRAY[5, 3, 7]);
WITH recent AS (SELECT * FROM events WHERE region > current_date - 15), ranked AS (
  SELECT *, row_number() OVER (PARTITION BY id ORDER BY name DESC) AS rn FROM recent)
SELECT r.*, (SELECT max(region) FROM events x WHERE x.id = r.id) FROM ranked r WHERE rn <= 5
UNION ALL SELECT * FROM sessions WHERE EXISTS (SELECT 1 FROM customers WHERE region = $2);
DELETE FROM events USING events WHERE orders.region = orders.id AND orders.region < now() - interval '1 year' RETURNING *;
SELECT a.amount, b.status AS "B Col", count(*) FILTER (WHERE a.owner_id > 371) AS n, coalesce(sum(a.status), 0)::numeric(12, 2)
  FROM events AS a LEFT JOIN invoices b ON b.id = a.score AND b.name IS NOT NULL
  WHERE a.name BETWEEN 58 AND 417 AND a.owner_id IN ('x', 'y', 'z') AND NOT a.status LIKE 'p%'
  GROUP BY 1, 2 HAVING count(*) > 2 ORDER BY n DESC, 1 LIMIT 9 OFFSET 550;
INSERT INTO events (score, created_at, score) VALUES (806105, 'text 988312', now() + interval '8 days'), (578847, E'it''s\n', NULL)
  ON CONFLICT (id) DO UPDATE SET status = EXCLUDED.name RETURNING id;
UPDATE products SET created_at = owner_id + 39, owner_id = CASE WHEN status > 800 THEN 'big' ELSE 'small' END WHERE id = $1 AND region <> ALL (ARRAY[3, 8, 0]);
WITH recent AS (SELECT * FROM invoices WHERE name > current_date - 15), ranked AS (
  SELECT *, row_number() OVER (PARTITION BY amount ORDER BY status DESC) AS rn FROM recent)
SELECT r.*, (SELECT max(region) FROM customers x WHERE x.id = r.id) FROM ranked r WHERE rn <= 5
UNION ALL SELECT * FROM events WHERE EXISTS (SELECT 1 FROM customers WHERE id = $2);
DELETE FROM events USING products WHERE orders.amount = orders.id AND orders.amount < now() - interval '1 year' RETURNING *;
SELECT a.name, b.created_at AS "B Col", count(*) FILTER (WHERE a.id > 554) AS n, coalesce(sum(a.score), 0)::numeric(12, 2)
  FROM customers AS a LEFT JOIN events b ON b.id = a.region AND b.amount IS NOT NULL
  WHERE a.created_at BETWEEN 48 AND 331 AND a.owner_id IN ('x', 'y', 'z') AND NOT a.region LIKE 'p%'
  GROUP BY 1, 2 HAVING count(*) > 8 ORDER BY n DESC, 1 LIMIT 4 OFFSET 916;
INSERT INTO invoices (owner_id, name, region) VALUES (44079, 'text 286909', now() + interval '24 days'), (457079, E'it''s\n', NULL)
  ON CONFLICT (id) DO UPDATE SET score = EXCLUDED.score RETURNING id;
UPDATE products SET id = id + 66, amount = CASE WHEN region > 117 THEN 'big' ELSE 'small' END WHERE id = $1 AND amount <> ALL (ARRAY[8, 0, 8]);
WITH recent AS (SELECT * FROM orders WHERE id > current_date - 33), ranked AS (
  SELECT *, row_number() OVER (PARTITION BY region ORDER BY owner_id DESC) AS rn FROM recent)
SELECT r.*, (SELECT max(created_at) FROM products x WHERE x.id = r.id) FROM ranked r WHERE rn <= 8
UNION ALL SELECT * FROM events WHERE EXISTS (SELECT 1 FROM products WHERE id = $2);
DELETE FROM orders USING events WHERE orders.status = orders.id AND orders.owner_id < now() - interval '1 year' RETURNING *;
SELECT a.score, b.owner_id AS "B Col", count(*) FILTER (WHERE a.id > 308) AS n, coalesce(sum(a.owner_id), 0)::numeric(12, 2)
  FROM events AS a LEFT JOIN sessions b ON b.id = a.amount AND b.name IS NOT NULL
  WHERE a.id BETWEEN 16 AND 125 AND a.region IN ('x', 'y', 'z') AND NOT a.region LIKE 'p%'
  GROUP BY 1, 2 HAVING count(*) > 3 ORDER BY n DESC, 1 LIMIT 53 OFFSET 296;
INSERT INTO events (created_at, amount, status) VALUES (230606, 'text 825338', now() + interval '25 days'), (171715, E'it''s\n', NULL)
  ON CONFLICT (id) DO UPDATE SET id = EXCLUDED.name RETURNING id;
UPDATE invoices SET score = status + 69, created_at = CASE WHEN region > 20 THEN 'big' ELSE 'small' END WHERE id = $1 AND name <> ALL (ARRAY[4, 4, 9]);
WITH recent AS (SELECT * FROM sessions WHERE created_at > current_date - 43), ranked AS (
  SELECT *, row_number() OVER (PARTITION BY id ORDER BY region DESC) AS rn FROM recent)
SELECT r.*, (SELECT max(region) FROM invoices x WHERE x.id = r.id) FROM ranked r WHERE rn <= 1
UNION ALL SELECT * FROM products WHERE EXISTS (SELECT 1 FROM customers WHERE region = $2);
DELETE FROM invoices USING invoices WHERE orders.id = orders.id AND orders.name < now() - interval '1 year' RETURNING *;
SELECT a.region, b.region AS "B Col", count(*) FILTER (WHERE a.id > 685) AS n, coalesce(sum(a.status), 0)::numeric(12, 2)
  FROM invoices AS a LEFT JOIN orders b ON b.id = a.score AND b.region IS NOT NULL
  WHERE a.score BETWEEN 75 AND 532 AND a.amount IN ('x', 'y', 'z') AND NOT a.owner_id LIKE 'p%'
  GROUP BY 1, 2 HAVING count(*) > 4 ORDER BY n DESC, 1 LIMIT 10 OFFSET 857;
INSERT INTO invoices (status, region, region) VALUES (219442, 'text 518354', now() + interval '11 days'), (566281, E'it''s\n', NULL)
  ON CONFLICT (id) DO UPDATE SET amount = EXCLUDED.owner_id RETURNING id;
UPDATE products SET score = status + 54, name = CASE WHEN id > 504 THEN 'big' ELSE 'small' END WHERE id = $1 AND created_at <> ALL (ARRAY[7, 4, 7]);
WITH recent AS (SELECT * FROM events WHERE created_at > current_date - 84), ranked AS (
  SELECT *, row_number() OVER (PARTITION BY id ORDER BY owner_id DESC) AS rn FROM recent)
SELECT r.*, (SELECT max(amount) FROM orders x WHERE x.id = r.id) FROM ranked r WHERE rn <= 7
UNION ALL SELECT * FROM customers WHERE EXISTS (SELECT 1 FROM sessions WHERE region = $2);
DELETE FROM events USING customers WHERE orders.id = orders.id AND orders.amount < now() - interval '1 year' RETURNING *;
SELECT a.created_at, b.created_at AS "B Col", count(*) FILTER (WHERE a.name > 486) AS n, coalesce(sum(a.owner_id), 0)::numeric(12, 2)
  FROM sessions AS a LEFT JOIN orders b ON b.id = a.name AND b.owner_id IS NOT NULL
  WHERE a.created_at BETWEEN 48 AND 221 AND a.score IN ('x', 'y', 'z') AND NOT a.created_at LIKE 'p%'
  GROUP BY 1, 2 HAVING count(*) > 3 ORDER BY n DESC, 1 LIMIT 82 OFFSET 504;
INSERT INTO events (score, amount, amount) VALUES (792283, 'text 744110', now() + interval '11 days'), (439354, E'it''s\n', NULL)
  ON CONFLICT (id) DO UPDATE SET owner_id = EXCLUDED.status RETURNING id;
UPDATE invoices SET region = name + 62, status = CASE WHEN score > 490 THEN 'big' ELSE 'small' END WHERE id = $1 AND owner_id <> ALL (ARRAY[8, 0, 2]);
WITH recent AS (SELECT * FROM products WHERE owner_id > current_date - 54), ranked AS (
  SELECT *, row_number() OVER (PARTITION BY id ORDER BY created_at DESC) AS rn FROM recent)
SELECT r.*, (SELECT max(id) FROM sessions x WHERE x.id = r.id) FROM ranked r WHERE rn <= 5
UNION ALL SELECT * FROM orders WHERE EXISTS (SELECT 1 FROM orders WHERE score = $2);
DELETE FROM sessions USING orders WHERE orders.id = orders.id AND orders.owner_id < now() - interval '1 year' RETURNING *;
SELECT a.status, b.owner_id AS "B Col", count(*) FILTER (WHERE a.score > 740) AS n, coalesce(sum(a.amount), 0)::numeric(12, 2)
  FROM sessions AS a LEFT JOIN invoices b ON b.id = a.id AND b.region IS NOT NULL
  WHERE a.name BETWEEN 70 AND 352 AND a.id IN ('x', 'y', 'z') AND NOT a.owner_id LIKE 'p%'
  GROUP BY 1, 2 HAVING count(*) > 8 ORDER BY n DESC, 1 LIMIT 49 OFFSET 965;
INSERT INTO orders (id, amount, region) VALUES (243041, 'text 977279', now() + interval '9 days'), (504921, E'it''s\n', NULL)
  ON CONFLICT (id) DO UPDATE SET owner_id = EXCLUDED.status RETURNING id;
UPDATE products SET score = id + 62, created_at = CASE WHEN score > 924 THEN 'big' ELSE 'small' END WHERE id = $1 AND region <> ALL (ARRAY[7, 3, 7]);
WITH recent AS (SELECT * FROM sessions WHERE name > current_date - 35), ranked AS (
  SELECT *, row_number() OVER (PARTITION BY owner_id ORDER BY region DESC) AS rn FROM recent)
SELECT r.*, (SELECT max(score) FROM events x WHERE x.id = r.id) FROM ranked r WHERE rn <= 3
UNION ALL SELECT * FROM orders WHERE EXISTS (SELECT 1 FROM sessions WHERE id = $2);
DELETE FROM sessions USING products WHERE orders.score = orders.id AND orders.owner_id < now() - interval '1 year' RETURNING *;
SELECT a.amount, b.created_at AS "B Col", count(*) FILTER (WHERE a.created_at > 767) AS n, coalesce(sum(a.score), 0)::numeric(12, 2)
  FROM invoices AS a LEFT JOIN orders b ON b.id = a.amount AND b.score IS NOT NULL
  WHERE a.score BETWEEN 56 AND 800 AND a.created_at IN ('x', 'y', 'z') AND NOT a.region LIKE 'p%'
  GROUP BY 1, 2 HAVING count(*) > 4 ORDER BY n DESC, 1 LIMIT 26 OFFSET 159;
INSERT INTO orders (status, created_at, region) VALUES (800655, 'text 288661', now() + interval '10 days'), (815494, E'it''s\n', NULL)
  ON CONFLICT (id) DO UPDATE SET name = EXCLUDED.status RETURNING id;
UPDATE customers SET name = owner_id + 53, owner_id = CASE WHEN owner_id > 642 THEN 'big' ELSE 'small' END WHERE id = $1 AND score <> ALL (ARRAY[3, 6, 3]);
WITH recent AS (SELECT * FROM products WHERE created_at > current_date - 4), ranked AS (
  SELECT *, row_number() OVER (PARTITION BY region ORDER BY name DESC) AS rn FROM recent)
SELECT r.*, (SELECT max(region) FROM events x WHERE x.id = r.id) FROM ranked r WHERE rn <= 9
UNION ALL SELECT * FROM sessions WHERE EXISTS (SELECT 1 FROM orders WHERE id = $2);
DELETE FROM products USING invoices WHERE orders.id = orders.id AND orders.score < now() - interval '1 year' RETURNING *;
SELECT a.score, b.region AS "B Col", count(*) FILTER (WHERE a.name > 484) AS n, coalesce(sum(a.score), 0)::numeric(12, 2)
  FROM orders AS a LEFT JOIN sessions b ON b.id = a.score AND b.id IS NOT NULL
  WHERE a.name BETWEEN 7 AND 805 AND a.amount IN ('x', 'y', 'z') AND NOT a.id LIKE 'p%'
  GROUP BY 1, 2 HAVING count(*) > 2 ORDER BY n DESC, 1 LIMIT 75 OFFSET 534;
INSERT INTO customers (id, created_at, status) VALUES (142184, 'text 722893', now() + interval '3 days'), (326695, E'it''s\n', NULL)
  ON CONFLICT (id) DO UPDATE SET id = EXCLUDED.id RETURNING id;
UPDATE invoices SET amount = status + 47, id = CASE WHEN owner_id > 689 THEN 'big' ELSE 'small' END WHERE id = $1 AND created_at <> ALL (ARRAY[2, 8, 5]);
WITH recent AS (SELECT * FROM products WHERE id > current_date - 26), ranked AS (
  SELECT *, row_number() OVER (PARTITION BY status ORDER BY created_at DESC) AS rn FROM recent)
SELECT r.*, (SELECT max(amount) FROM sessions x WHERE x.id = r.id) FROM ranked r WHERE rn <= 5
UNION ALL SELECT * FROM invoices WHERE EXISTS (SELECT 1 FROM products WHERE name = $2);
DELETE FROM products USING sessions WHERE orders.score = orders.id AND orders.name < now() - interval '1 year' RETURNING *;
SELECT a.status, b.owner_id AS "B Col", count(*) FILTER (WHERE a.name > 203) AS n, coalesce(sum(a.status), 0)::numeric(12, 2)
  FROM orders AS a LEFT JOIN orders b ON b.id = a.score AND b.status IS NOT NULL
  WHERE a.region BETWEEN 47 AND 385 AND a.created_at IN ('x', 'y', 'z') AND NOT a.id LIKE 'p%'
  GROUP BY 1, 2 HAVING count(*) > 9 ORDER BY n DESC, 1 LIMIT 64 OFFSET 559;
INSERT INTO orders (owner_id, score, name) VALUES (896551, 'text 615430', now() + interval '27 days'), (647903, E'it''s\n', NULL)
  ON CONFLICT (id) DO UPDATE SET owner_id = EXCLUDED.created_at RETURNING id;
UPDATE events SET amount = region + 80, score = CASE WHEN status > 448 THEN 'big' ELSE 'small' END WHERE id = $1 AND score <> ALL (ARRAY[8, 2, 8]);
WITH recent AS (SELECT * FROM events WHERE name > current_date - 48), ranked AS (
  SELECT *, row_number() OVER (PARTITION BY region ORDER BY amount DESC) AS rn FROM recent)
SELECT r.*, (SELECT max(name) FROM orders x WHERE x.id = r.id) FROM ranked r WHERE rn <= 4
UNION ALL SELECT * FROM orders WHERE EXISTS (SELECT 1 FROM customers WHERE id = $2);
DELETE FROM sessions USING events WHERE orders.name = orders.id AND orders.region < now() - interval '1 year' RETURNING *;
SELECT a.name, b.owner_id AS "B Col", count(*) FILTER (WHERE a.score > 336) AS n, coalesce(sum(a.region), 0)::numeric(12, 2)
  FROM orders AS a LEFT JOIN products b ON b.id = a.owner_id AND b.region IS NOT NULL
  WHERE a.status BETWEEN 13 AND 595 AND a.name IN ('x', 'y', 'z') AND NOT a.amount LIKE 'p%'
  GROUP BY 1, 2 HAVING count(*) > 5 ORDER BY n DESC, 1 LIMIT 0 OFFSET 511;
INSERT INTO customers (score, id, status) VALUES (400747, 'text 629993', now() + interval '28 days'), (228601, E'it''s\n', NULL)
  ON CONFLICT (id) DO UPDATE SET score = EXCLUDED.id RETURNING id;
UPDATE invoices SET score = region + 91, status = CASE WHEN amount > 644 THEN 'big' ELSE 'small' END WHERE id = $1 AND amount <> ALL (ARRAY[9, 9, 6]);
WITH recent AS (SELECT * FROM orders WHERE created_at > current_date - 61), ranked AS (
  SELECT *, row_number() OVER (PARTITION BY name ORDER BY region DESC) AS rn FROM recent)
SELECT r.*, (SELECT max(created_at) FROM customers x WHERE x.id = r.id) FROM ranked r WHERE rn <= 6
UNION ALL SELECT * FROM products WHERE EXISTS (SELECT 1 FROM events WHERE owner_id = $2);
DELETE FROM events USING sessions WHERE orders.status = orders.id AND orders.created_at < now() - interval '1 year' RETURNING *;
SELECT a.status, b.owner_id AS "B Col", count(*) FILTER (WHERE a.status > 566) AS n, coalesce(sum(a.region), 0)::numeric(12, 2)
  FROM customers AS a LEFT JOIN customers b ON b.id = a.score AND b.score IS NOT NULL
  WHERE a.region BETWEEN 83 AND 479 AND a.status IN ('x', 'y', 'z') AND NOT a.region LIKE 'p%'
  GROUP BY 1, 2 HAVING count(*) > 0 ORDER BY n DESC, 1 LIMIT 46 OFFSET 994;
INSERT INTO sessions (name, region, owner_id) VALUES (277779, 'text 940004', now() + interval '19 days'), (59092, E'it''s\n', NULL)
  ON CONFLICT (id) DO UPDATE SET status = EXCLUDED.created_at RETURNING id;
UPDATE invoices SET amount = status + 84, created_at = CASE WHEN score > 225 THEN 'big' ELSE 'small' END WHERE id = $1 AND name <> ALL (ARRAY[7, 1, 3]);
WITH recent AS (SELECT * FROM events WHERE region > current_date - 88), ranked AS (
  SELECT *, row_number() OVER (PARTITION BY amount ORDER BY created_at DESC) AS rn FROM recent)
SELECT r.*, (SELECT max(status) FROM products x WHERE x.id = r.id) FROM ranked r WHERE rn <= 9
UNION ALL SELECT * FROM orders WHERE EXISTS (SELECT 1 FROM sessions WHERE region = $2);
DELETE FROM orders USING products WHERE orders.name = orders.id AND orders.amount < now() - interval '1 year' RETURNING *;
SELECT a.name, b.id AS "B Col", count(*) FILTER (WHERE a.region > 846) AS n, coalesce(sum(a.id), 0)::numeric(12, 2)
  FROM customers AS a LEFT JOIN customers b ON b.id = a.name AND b.owner_id IS NOT NULL
  WHERE a.created_at BETWEEN 63 AND 842 AND a.region IN ('x', 'y', 'z') AND NOT a.status LIKE 'p%'
  GROUP BY 1, 2 HAVING count(*) > 4 ORDER BY n DESC, 1 LIMIT 90 OFFSET 187;
INSERT INTO invoices (amount, name, score) VALUES (871708, 'text 351252', now() + interval '8 days'), (8878, E'it''s\n', NULL)
  ON CONFLICT (id) DO UPDATE SET status = EXCLUDED.score RETURNING id;
UPDATE products SET created_at = created_at + 28, created_at = CASE WHEN region > 247 THEN 'big' ELSE 'small' END WHERE id = $1 AND id <> ALL (ARRAY[4, 6, 1]);
WITH recent AS (SELECT * FROM sessions WHERE id > current_date - 60), ranked AS (
  SELECT *, row_number() OVER (PARTITION BY amount ORDER BY region DESC) AS rn FROM recent)
SELECT r.*, (SELECT max(owner_id) FROM sessions x WHERE x.id = r.id) FROM ranked r WHERE rn <= 8
UNION ALL SELECT * FROM products WHERE EXISTS (SELECT 1 FROM events WHERE name = $2);
DELETE FROM orders USING customers WHERE orders.owner_id = orders.id AND orders.amount < now() - interval '1 year' RETURNING *;
SELECT a.status, b.amount AS "B Col", count(*) FILTER (WHERE a.name > 321) AS n, coalesce(sum(a.status), 0)::numeric(12, 2)
  FROM orders AS a LEFT JOIN orders b ON b.id = a.created_at AND b.status IS NOT NULL
  WHERE a.region BETWEEN 86 AND 818 AND a.id IN ('x', 'y', 'z') AND NOT a.created_at LIKE 'p%'
  GROUP BY 1, 2 HAVING count(*) > 3 ORDER BY n DESC, 1 LIMIT 77 OFFSET 668;
INSERT INTO orders (owner_id, name, region) VALUES (991926, 'text 934994', now() + interval '8 days'), (458746, E'it''s\n', NULL)
  ON CONFLICT (id) DO UPDATE SET score = EXCLUDED.score RETURNING id;
UPDATE customers SET amount = region + 9, score = CASE WHEN score > 182 THEN 'big' ELSE 'small' END WHERE id = $1 AND score <> ALL (ARRAY[9, 2, 2]);
WITH recent AS (SELECT * FROM events WHERE region > current_date - 79), ranked AS (
  SELECT *, row_number() OVER (PARTITION BY status ORDER BY created_at DESC) AS rn FROM recent)
SELECT r.*, (SELECT max(region) FROM orders x WHERE x.id = r.id) FROM ranked r WHERE rn <= 0
UNION ALL SELECT * FROM events WHERE EXISTS (SELECT 1 FROM invoices WHERE created_at = $2);
DELETE FROM events USING sessions WHERE orders.name = orders.id AND orders.id < now() - interval '1 year' RETURNING *;
SELECT a.amount, b.score AS "B Col", count(*) FILTER (WHERE a.created_at > 50) AS n, coalesce(sum(a.score), 0)::numeric(12, 2)
  FROM products AS a LEFT JOIN products b ON b.id = a.id AND b.id IS NOT NULL
  WHERE a.amount BETWEEN 6 AND 836 AND a.created_at IN ('x', 'y', 'z') AND NOT a.status LIKE 'p%'
  GROUP BY 1, 2 HAVING count(*) > 6 ORDER BY n DESC, 1 LIMIT 86 OFFSET 569;
INSERT INTO events (amount, score, score) VALUES (675408, 'text 272293', now() + interval '4 days'), (715775, E'it''s\n', NULL)
  ON CONFLICT (id) DO UPDATE SET id = EXCLUDED.owner_id RETURNING id;
UPDATE sessions SET owner_id = status + 21, region = CASE WHEN status > 604 THEN 'big' ELSE 'small' END WHERE id = $1 AND name <> ALL (ARRAY[4, 8, 6]);
WITH recent AS (SELECT * FROM events WHERE region > current_date - 46), ranked AS (
  SELECT *, row_number() OVER (PARTITION BY owner_id ORDER BY score DESC) AS rn FROM recent)
SELECT r.*, (SELECT max(region) FROM sessions x WHERE x.id = r.id) FROM ranked r WHERE rn <= 2
UNION ALL SELECT * FROM products WHERE EXISTS (SELECT 1 FROM invoices WHERE amount = $2);
DELETE FROM sessions USING events WHERE orders.id = orders.id AND orders.amount < now() - interval '1 year' RETURNING *;
SELECT a.id, b.amount AS "B Col", count(*) FILTER (WHERE a.region > 422) AS n, coalesce(sum(a.id), 0)::numeric(12, 2)
  FROM invoices AS a LEFT JOIN customers b ON b.id = a.score AND b.status IS NOT NULL
  WHERE a.status BETWEEN 31 AND 329 AND a.status IN ('x', 'y', 'z') AND NOT a.owner_id LIKE 'p%'
  GROUP BY 1, 2 HAVING count(*) > 5 ORDER BY n DESC, 1 LIMIT 29 OFFSET 964;
INSERT INTO products (region, owner_id, amount) VALUES (797573, 'text 693138', now() + interval '20 days'), (755923, E'it''s\n', NULL)
  ON CONFLICT (id) DO UPDATE SET score = EXCLUDED.name RETURNING id;
UPDATE sessions SET amount = status + 66, score = CASE WHEN score > 76 THEN 'big' ELSE 'small' END WHERE id = $1 AND created_at <> ALL (ARRAY[0, 0, 2]);
WITH recent AS (SELECT * FROM orders WHERE status > current_date - 68), ranked AS (
  SELECT *, row_number() OVER (PARTITION BY score ORDER BY status DESC) AS rn FROM recent)
SELECT r.*, (SELECT max(region) FROM orders x WHERE x.id = r.id) FROM ranked r WHERE rn <= 6
UNION ALL SELECT * FROM events WHERE EXISTS (SELECT 1 FROM customers WHERE amount = $2);
DELETE FROM products USING sessions WHERE orders.region = orders.id AND orders.name < now() - interval '1 year' RETURNING *;
SELECT a.owner_id, b.status AS "B Col", count(*) FILTER (WHERE a.status > 181) AS n, coalesce(sum(a.created_at), 0)::numeric(12, 2)
  FROM products AS a LEFT JOIN products b ON b.id = a.status AND b.id IS NOT NULL
  WHERE a.region BETWEEN 95 AND 871 AND a.amount IN ('x', 'y', 'z') AND NOT a.id LIKE 'p%'
  GROUP BY 1, 2 HAVING count(*) > 9 ORDER BY n DESC, 1 LIMIT 14 OFFSET 991;
INSERT INTO events (region, status, id) VALUES (782382, 'text 242065', now() + interval '24 days'), (453088, E'it''s\n', NULL)
  ON CONFLICT (id) DO UPDATE SET id = EXCLUDED.score RETURNING id;
UPDATE invoices SET region = owner_id + 44, status = CASE WHEN name > 834 THEN 'big' ELSE 'small' END WHERE id = $1 AND score <> ALL (ARRAY[9, 7, 8]);
WITH recent AS (SELECT * FROM sessions WHERE region > current_date - 85), ranked AS (
  SELECT *, row_number() OVER (PARTITION BY id ORDER BY amount DESC) AS rn FROM recent)
SELECT r.*, (SELECT max(owner_id) FROM products x WHERE x.id = r.id) FROM ranked r WHERE rn <= 7
UNION ALL SELECT * FROM invoices WHERE EXISTS (SELECT 1 FROM products WHERE owner_id = $2);
DELETE FROM orders USING orders WHERE orders.status = orders.id AND orders.score < now() - interval '1 year' RETURNING *;
SELECT a.region, b.name AS "B Col", count(*) FILTER (WHERE a.id > 957) AS n, coalesce(sum(a.id), 0)::numeric(12, 2)
  FROM customers AS a LEFT JOIN sessions b ON b.id = a.region AND b.id IS NOT NULL
  WHERE a.name BETWEEN 74 AND 307 AND a.id IN ('x', 'y', 'z') AND NOT a.status LIKE 'p%'
  GROUP BY 1, 2 HAVING count(*) > 7 ORDER BY n DESC, 1 LIMIT 67 OFFSET 179;
INSERT INTO events (status, created_at, region) VALUES (51597, 'text 498002', now() + interval '12 days'), (673571, E'it''s\n', NULL)
  ON CONFLICT (id) DO UPDATE SET status = EXCLUDED.region RETURNING id;
UPDATE sessions SET amount = score + 13, owner_id = CASE WHEN score > 588 THEN 'big' ELSE 'small' END WHERE id = $1 AND name <> ALL (ARRAY[4, 3, 9]);
WITH recent AS (SELECT * FROM customers WHERE id > current_date - 46), ranked AS (
  SELECT *, row_number() OVER (PARTITION BY region ORDER BY name DESC) AS rn FROM recent)
SELECT r.*, (SELECT max(name) FROM invoices x WHERE x.id = r.id) FROM ranked r WHERE rn <= 3
UNION ALL SELECT * FROM customers WHERE EXISTS (SELECT 1 FROM invoices WHERE score = $2);
DELETE FROM customers USING customers WHERE orders.owner_id = orders.id AND orders.score < now() - interval '1 year' RETURNING *;
SELECT a.amount, b.score AS "B Col", count(*) FILTER (WHERE a.amount > 29) AS n, coalesce(sum(a.score), 0)::numeric(12, 2)
  FROM events AS a LEFT JOIN invoices b ON b.id = a.region AND b.id IS NOT NULL
  WHERE a.id BETWEEN 58 AND 1072 AND a.status IN ('x', 'y', 'z') AND NOT a.created_at LIKE 'p%'
  GROUP BY 1, 2 HAVING count(*) > 5 ORDER BY n DESC, 1 LIMIT 85 OFFSET 727;
INSERT INTO products (owner_id, score, score) VALUES (980779, 'text 912578', now() + interval '17 days'), (982796, E'it''s\n', NULL)
  ON CONFLICT (id) DO UPDATE SET amount = EXCLUDED.created_at RETURNING id;
UPDATE products SET created_at = region + 89, score = CASE WHEN region > 163 THEN 'big' ELSE 'small' END WHERE id = $1 AND id <> ALL (ARRAY[8, 0, 6]);
WITH recent AS (SELECT * FROM sessions WHERE id > current_date - 11), ranked AS (
  SELECT *, row_number() OVER (PARTITION BY created_at ORDER BY region DESC) AS rn FROM recent)
SELECT r.*, (SELECT max(status) FROM products x WHERE x.id = r.id) FROM ranked r WHERE rn <= 6
UNION ALL SELECT * FROM customers WHERE EXISTS (SELECT 1 FROM invoices WHERE name = $2);
DELETE FROM orders USING events WHERE orders.status = orders.id AND orders.name < now() - interval '1 year' RETURNING *;
SELECT a.name, b.amount AS "B Col", count(*) FILTER (WHERE a.status > 979) AS n, coalesce(sum(a.score), 0)::numeric(12, 2)
  FROM products AS a LEFT JOIN products b ON b.id = a.id AND b.created_at IS NOT NULL
  WHERE a.owner_id BETWEEN 55 AND 1077 AND a.id IN ('x', 'y', 'z') AND NOT a.score LIKE 'p%'
  GROUP BY 1, 2 HAVING count(*) > 4 ORDER BY n DESC, 1 LIMIT 98 OFFSET 74;
INSERT INTO products (created_at, amount, region) VALUES (796818, 'text 210761', now() + interval '3 days'), (435329, E'it''s\n', NULL)
  ON CONFLICT (id) DO UPDATE SET amount = EXCLUDED.id RETURNING id;
UPDATE customers SET created_at = amount + 6, status = CASE WHEN status > 80 THEN 'big' ELSE 'small' END WHERE id = $1 AND amount <> ALL (ARRAY[9, 6, 5]);
WITH recent AS (SELECT * FROM events WHERE status > current_date - 68), ranked AS (
  SELECT *, row_number() OVER (PARTITION BY score ORDER BY created_at DESC) AS rn FROM recent)
SELECT r.*, (SELECT max(score) FROM sessions x WHERE x.id = r.id) FROM ranked r WHERE rn <= 4
UNION ALL SELECT * FROM customers WHERE EXISTS (SELECT 1 FROM products WHERE id = $2);
DELETE FROM customers USING invoices WHERE orders.name = orders.id AND orders.created_at < now() - interval '1 year' RETURNING *;
SELECT a.created_at, b.name AS "B Col", count(*) FILTER (WHERE a.region > 481) AS n, coalesce(sum(a.status), 0)::numeric(12, 2)
  FROM events AS a LEFT JOIN events b ON b.id = a.score AND b.amount IS NOT NULL
  WHERE a.score BETWEEN 41 AND 959 AND a.amount IN ('x', 'y', 'z') AND NOT a.amount LIKE 'p%'
  GROUP BY 1, 2 HAVING count(*) > 8 ORDER BY n DESC, 1 LIMIT 85 OFFSET 48;
INSERT INTO events (created_at, created_at, name) VALUES (876629, 'text 589776', now() + interval '15 days'), (54275, E'it''s\n', NULL)
  ON CONFLICT (id) DO UPDATE SET status = EXCLUDED.name RETURNING id;
UPDATE events SET name = status + 53, created_at = CASE WHEN score > 790 THEN 'big' ELSE 'small' END WHERE id = $1 AND created_at <> ALL (ARRAY[4, 1, 3]);
WITH recent AS (SELECT * FROM sessions WHERE region > current_date - 48), ranked AS (
  SELECT *, row_number() OVER (PARTITION BY score ORDER BY region DESC) AS rn FROM recent)
SELECT r.*, (SELECT max(score) FROM orders x WHERE x.id = r.id) FROM ranked r WHERE rn <= 1
UNION ALL SELECT * FROM invoices WHERE EXISTS (SELECT 1 FROM customers WHERE id = $2);
DELETE FROM customers USING orders WHERE orders.owner_id = orders.id AND orders.created_at < now() - interval '1 year' RETURNING *;
SELECT a.id, b.status AS "B Col", count(*) FILTER (WHERE a.created_at > 454) AS n, coalesce(sum(a.region), 0)::numeric(12, 2)
  FROM sessions AS a LEFT JOIN products b ON b.id = a.score AND b.score IS NOT NULL
  WHERE a.status BETWEEN 86 AND 217 AND a.status IN ('x', 'y', 'z') AND NOT a.created_at LIKE 'p%'
  GROUP BY 1, 2 HAVING count(*) > 6 ORDER BY n DESC, 1 LIMIT 91 OFFSET 879;
INSERT INTO orders (owner_id, id, id) VALUES (664790, 'text 295550', now() + interval '22 days'), (323728, E'it''s\n', NULL)
  ON CONFLICT (id) DO UPDATE SET id = EXCLUDED.region RETURNING id;
UPDATE sessions SET amount = score + 57, region = CASE WHEN id > 750 THEN 'big' ELSE 'small' END WHERE id = $1 AND region <> ALL (ARRAY[6, 5, 5]);
WITH recent AS (SELECT * FROM events WHERE status > current_date - 54), ranked AS (
  SELECT *, row_number() OVER (PARTITION BY region ORDER BY score DESC) AS rn FROM recent)
SELECT r.*, (SELECT max(owner_id) FROM orders x WHERE x.id = r.id) FROM ranked r WHERE rn <= 7
UNION ALL SELECT * FROM sessions WHERE EXISTS (SELECT 1 FROM invoices WHERE amount = $2);
DELETE FROM products USING customers WHERE orders.name = orders.id AND orders.amount < now() - interval '1 year' RETURNING *;
SELECT a.id, b.owner_id AS "B Col", count(*) FILTER (WHERE a.id > 917) AS n, coalesce(sum(a.score), 0)::numeric(12, 2)
  FROM customers AS a LEFT JOIN orders b ON b.id = a.id AND b.amount IS NOT NULL
  WHERE a.amount BETWEEN 40 AND 999 AND a.amount IN ('x', 'y', 'z') AND NOT a.owner_id LIKE 'p%'
  GROUP BY 1, 2 HAVING count(*) > 4 ORDER BY n DESC, 1 LIMIT 77 OFFSET 838;
INSERT INTO customers (status, amount, score) VALUES (897968, 'text 38479', now() + interval '0 days'), (270273, E'it''s\n', NULL)
  ON CONFLICT (id) DO UPDATE SET id = EXCLUDED.status RETURNING id;
UPDATE customers SET score = score + 43, score = CASE WHEN score > 703 THEN 'big' ELSE 'small' END WHERE id = $1 AND score <> ALL (ARRAY[3, 5, 6]);
WITH recent AS (SELECT * FROM events WHERE status > current_date - 11), ranked AS (
  SELECT *, row_number() OVER (PARTITION BY region ORDER BY score DESC) AS rn FROM recent)
SELECT r.*, (SELECT max(created_at) FROM products x WHERE x.id = r.id) FROM ranked r WHERE rn <= 7
UNION ALL SELECT * FROM invoices WHERE EXISTS (SELECT 1 FROM customers WHERE owner_id = $2);
DELETE FROM products USING orders WHERE orders.owner_id = orders.id AND orders.created_at < now() - interval '1 year' RETURNING *;
SELECT a.name, b.owner_id AS "B Col", count(*) FILTER (WHERE a.name > 971) AS n, coalesce(sum(a.id), 0)::numeric(12, 2)
  FROM orders AS a LEFT JOIN invoices b ON b.id = a.created_at AND b.id IS NOT NULL
  WHERE a.score BETWEEN 28 AND 374 AND a.amount IN ('x', 'y', 'z') AND NOT a.created_at LIKE 'p%'
  GROUP BY 1, 2 HAVING count(*) > 3 ORDER BY n DESC, 1 LIMIT 72 OFFSET 75;
INSERT INTO customers (id, id, status) VALUES (986057, 'text 464593', now() + interval '6 days'), (81210, E'it''s\n', NULL)
  ON CONFLICT (id) DO UPDATE SET owner_id = EXCLUDED.owner_id RETURNING id;
UPDATE invoices SET score = owner_id + 56, created_at = CASE WHEN status > 380 THEN 'big' ELSE 'small' END WHERE id = $1 AND amount <> ALL (ARRAY[0, 1, 7]);
WITH recent AS (SELECT * FROM invoices WHERE status > current_date - 41), ranked AS (
  SELECT *, row_number() OVER (PARTITION BY id ORDER BY region DESC) AS rn FROM recent)
SELECT r.*, (SELECT max(owner_id) FROM customers x WHERE x.id = r.id) FROM ranked r WHERE rn <= 7
UNION ALL SELECT * FROM orders WHERE EXISTS (SELECT 1 FROM sessions WHERE amount = $2);
DELETE FROM sessions USING orders WHERE orders.region = orders.id AND orders.created_at < now() - interval '1 year' RETURNING *;
SELECT a.region, b.status AS "B Col", count(*) FILTER (WHERE a.owner_id > 227) AS n, coalesce(sum(a.name), 0)::numeric(12, 2)
  FROM orders AS a LEFT JOIN customers b ON b.id = a.name AND b.status IS NOT NULL
  WHERE a.id BETWEEN 67 AND 192 AND a.created_at IN ('x', 'y', 'z') AND NOT a.name LIKE 'p%'
  GROUP BY 1, 2 HAVING count(*) > 3 ORDER BY n DESC, 1 LIMIT 45 OFFSET 410;
INSERT INTO orders (id, score, status) VALUES (540062, 'text 503588', now() + interval '9 days'), (709192, E'it''s\n', NULL)
  ON CONFLICT (id) DO UPDATE SET id = EXCLUDED.owner_id RETURNING id;
UPDATE orders SET created_at = score + 4, created_at = CASE WHEN status > 203 THEN 'big' ELSE 'small' END WHERE id = $1 AND id <> ALL (ARRAY[5, 1, 0]);
WITH recent AS (SELECT * FROM orders WHERE id > current_date - 26), ranked AS (
  SELECT *, row_number() OVER (PARTITION BY created_at ORDER BY id DESC) AS rn FROM recent)
SELECT r.*, (SELECT max(owner_id) FROM products x WHERE x.id = r.id) FROM ranked r WHERE rn <= 3
UNION ALL SELECT * FROM events WHERE EXISTS (SELECT 1 FROM customers WHERE score = $2);
DELETE FROM sessions USING orders WHERE orders.created_at = orders.id AND orders.amount < now() - interval '1 year' RETURNING *;
SELECT a.score, b.score AS "B Col", count(*) FILTER (WHERE a.name > 604) AS n, coalesce(sum(a.status), 0)::numeric(12, 2)
  FROM orders AS a LEFT JOIN products b ON b.id = a.amount AND b.region IS NOT NULL
  WHERE a.created_at BETWEEN 23 AND 1039 AND a.status IN ('x', 'y', 'z') AND NOT a.created_at LIKE 'p%'
  GROUP BY 1, 2 HAVING count(*) > 7 ORDER BY n DESC, 1 LIMIT 16 OFFSET 478;
INSERT INTO invoices (name, owner_id, status) VALUES (956540, 'text 126310', now() + interval '17 days'), (385164, E'it''s\n', NULL)
  ON CONFLICT (id) DO UPDATE SET id = EXCLUDED.created_at RETURNING id;
UPDATE sessions SET score = name + 21, status = CASE WHEN id > 841 THEN 'big' ELSE 'small' END WHERE id = $1 AND region <> ALL (ARRAY[9, 0, 9]);
WITH recent AS (SELECT * FROM invoices WHERE id > current_date - 56), ranked AS (
  SELECT *, row_number() OVER (PARTITION BY status ORDER BY created_at DESC) AS rn FROM recent)
SELECT r.*, (SELECT max(id) FROM orders x WHERE x.id = r.id) FROM ranked r WHERE rn <= 7
UNION ALL SELECT * FROM customers WHERE EXISTS (SELECT 1 FROM invoices WHERE created_at = $2);
DELETE FROM products USING orders WHERE orders.status = orders.id AND orders.name < now() - interval '1 year' RETURNING *;
SELECT a.name, b.id AS "B Col", count(*) FILTER (WHERE a.score > 913) AS n, coalesce(sum(a.name), 0)::numeric(12, 2)
  FROM sessions AS a LEFT JOIN events b ON b.id = a.status AND b.created_at IS NOT NULL
  WHERE a.name BETWEEN 85 AND 835 AND a.score IN ('x', 'y', 'z') AND NOT a.name LIKE 'p%'
  GROUP BY 1, 2 HAVING count(*) > 8 ORDER BY n DESC, 1 LIMIT 32 OFFSET 559;
INSERT INTO customers (owner_id, id, amount) VALUES (153738, 'text 549313', now() + interval '22 days'), (112608, E'it''s\n', NULL)
  ON CONFLICT (id) DO UPDATE SET name = EXCLUDED.region RETURNING id;
UPDATE customers SET name = created_at + 96, owner_id = CASE WHEN amount > 97 THEN 'big' ELSE 'small' END WHERE id = $1 AND region <> ALL (ARRAY[8, 6, 1]);
WITH recent AS (SELECT * FROM customers WHERE region > current_date - 14), ranked AS (
  SELECT *, row_number() OVER (PARTITION BY created_at ORDER BY amount DESC) AS rn FROM recent)
SELECT r.*, (SELECT max(owner_id) FROM orders x WHERE x.id = r.id) FROM ranked r WHERE rn <= 1
UNION ALL SELECT * FROM events WHERE EXISTS (SELECT 1 FROM customers WHERE region = $2);
DELETE FROM events USING sessions WHERE orders.region = orders.id AND orders.status < now() - interval '1 year' RETURNING *;
SELECT a.amount, b.score AS "B Col", count(*) FILTER (WHERE a.created_at > 287) AS n, coalesce(sum(a.region), 0)::numeric(12, 2)
  FROM events AS a LEFT JOIN sessions b ON b.id = a.amount AND b.owner_id IS NOT NULL
  WHERE a.name BETWEEN 31 AND 753 AND a.region IN ('x', 'y', 'z') AND NOT a.amount LIKE 'p%'
  GROUP BY 1, 2 HAVING count(*) > 3 ORDER BY n DESC, 1 LIMIT 31 OFFSET 724;
INSERT INTO orders (owner_id, owner_id, created_at) VALUES (415433, 'text 709126', now() + interval '3 days'), (883192, E'it''s\n', NULL)
  ON CONFLICT (id) DO UPDATE SET status = EXCLUDED.owner_id RETURNING id;
UPDATE invoices SET created_at = name + 38, owner_id = CASE WHEN owner_id > 211 THEN 'big' ELSE 'small' END WHERE id = $1 AND score <> ALL (ARRAY[7, 7, 8]);
WITH recent AS (SELECT * FROM products WHERE created_at > current_date - 84), ranked AS (
  SELECT *, row_number() OVER (PARTITION BY id ORDER BY id DESC) AS rn FROM recent)
SELECT r.*, (SELECT max(score) FROM customers x WHERE x.id = r.id) FROM ranked r WHERE rn <= 6
UNION ALL SELECT * FROM orders WHERE EXISTS (SELECT 1 FROM invoices WHERE score = $2);
DELETE FROM customers USING events WHERE orders.owner_id = orders.id AND orders.amount < now() - interval '1 year' RETURNING *;
SELECT a.owner_id, b.created_at AS "B Col", count(*) FILTER (WHERE a.score > 401) AS n, coalesce(sum(a.status), 0)::numeric(12, 2)
  FROM orders AS a LEFT JOIN customers b ON b.id = a.created_at AND b.status IS NOT NULL
  WHERE a.score BETWEEN 57 AND 647 AND a.score IN ('x', 'y', 'z') AND NOT a.status LIKE 'p%'
  GROUP BY 1, 2 HAVING count(*) > 1 ORDER BY n DESC, 1 LIMIT 5 OFFSET 51;
INSERT INTO orders (owner_id, name, status) VALUES (712688, 'text 903959', now() + interval '21 days'), (241765, E'it''s\n', NULL)
  ON CONFLICT (id) DO UPDATE SET amount = EXCLUDED.owner_id RETURNING id;
UPDATE sessions SET owner_id = name + 41, region = CASE WHEN status > 56 THEN 'big' ELSE 'small' END WHERE id = $1 AND status <> ALL (ARRAY[9, 8, 6]);
WITH recent AS (SELECT * FROM invoices WHERE name > current_date - 42), ranked AS (
  SELECT *, row_number() OVER (PARTITION BY name ORDER BY amount DESC) AS rn FROM recent)
SELECT r.*, (SELECT max(score) FROM events x WHERE x.id = r.id) FROM ranked r WHERE rn <= 5
UNION ALL SELECT * FROM customers WHERE EXISTS (SELECT 1 FROM products WHERE id = $2);
DELETE FROM customers USING products WHERE orders.region = orders.id AND orders.status < now() - interval '1 year' RETURNING *;
SELECT a.name, b.name AS "B Col", count(*) FILTER (WHERE a.owner_id > 323) AS n, coalesce(sum(a.owner_id), 0)::numeric(12, 2)
  FROM customers AS a LEFT JOIN customers b ON b.id = a.status AND b.id IS NOT NULL
  WHERE a.score BETWEEN 98 AND 893 AND a.amount IN ('x', 'y', 'z') AND NOT a.name LIKE 'p%'
  GROUP BY 1, 2 HAVING count(*) > 5 ORDER BY n DESC, 1 LIMIT 99 OFFSET 297;
INSERT INTO events (amount, owner_id, amount) VALUES (419156, 'text 212017', now() + interval '4 days'), (469794, E'it''s\n', NULL)
  ON CONFLICT (id) DO UPDATE SET id = EXCLUDED.region RETURNING id;
UPDATE events SET id = id + 98, created_at = CASE WHEN status > 718 THEN 'big' ELSE 'small' END WHERE id = $1 AND owner_id <> ALL (ARRAY[6, 7, 1]);
WITH recent AS (SELECT * FROM invoices WHERE region > current_date - 31), ranked AS (
  SELECT *, row_number() OVER (PARTITION BY name ORDER BY id DESC) AS rn FROM recent)
SELECT r.*, (SELECT max(region) FROM orders x WHERE x.id = r.id) FROM ranked r WHERE rn <= 2
UNION ALL SELECT * FROM customers WHERE EXISTS (SELECT 1 FROM products WHERE owner_id = $2);
DELETE FROM products USING customers WHERE orders.owner_id = orders.id AND orders.status < now() - interval '1 year' RETURNING *;
SELECT a.id, b.owner_id AS "B Col", count(*) FILTER (WHERE a.owner_id > 471) AS n, coalesce(sum(a.score), 0)::numeric(12, 2)
  FROM events AS a LEFT JOIN customers b ON b.id = a.amount AND b.status IS NOT NULL
  WHERE a.score BETWEEN 63 AND 454 AND a.status IN ('x', 'y', 'z') AND NOT a.amount LIKE 'p%'
  GROUP BY 1, 2 HAVING count(*) > 4 ORDER BY n DESC, 1 LIMIT 67 OFFSET 664;
INSERT INTO events (status, id, amount) VALUES (291204, 'text 584201', now() + interval '14 days'), (255751, E'it''s\n', NULL)
  ON CONFLICT (id) DO UPDATE SET score = EXCLUDED.id RETURNING id;
UPDATE sessions SET owner_id = owner_id + 73, status = CASE WHEN amount > 93 THEN 'big' ELSE 'small' END WHERE id = $1 AND id <> ALL (ARRAY[9, 2, 6]);
WITH recent AS (SELECT * FROM sessions WHERE created_at > current_date - 75), ranked AS (
  SELECT *, row_number() OVER (PARTITION BY id ORDER BY amount DESC) AS rn FROM recent)
SELECT r.*, (SELECT max(name) FROM products x WHERE x.id = r.id) FROM ranked r WHERE rn <= 9
UNION ALL SELECT * FROM orders WHERE EXISTS (SELECT 1 FROM events WHERE region = $2);
DELETE FROM sessions USING products WHERE orders.created_at = orders.id AND orders.status < now() - interval '1 year' RETURNING *;
SELECT a.owner_id, b.created_at AS "B Col", count(*) FILTER (WHERE a.amount > 28) AS n, coalesce(sum(a.created_at), 0)::numeric(12, 2)
  FROM orders AS a LEFT JOIN products b ON b.id = a.status AND b.status IS NOT NULL
  WHERE a.region BETWEEN 48 AND 195 AND a.status IN ('x', 'y', 'z') AND NOT a.status LIKE 'p%'
  GROUP BY 1, 2 HAVING count(*) > 7 ORDER BY n DESC, 1 LIMIT 16 OFFSET 63;
INSERT INTO invoices (name, status, status) VALUES (612187, 'text 129015', now() + interval '25 days'), (174317, E'it''s\n', NULL)
  ON CONFLICT (id) DO UPDATE SET name = EXCLUDED.name RETURNING id;
UPDATE invoices SET created_at = id + 38, status = CASE WHEN score > 552 THEN 'big' ELSE 'small' END WHERE id = $1 AND id <> ALL (ARRAY[4, 9, 8]);
WITH recent AS (SELECT * FROM orders WHERE name > current_date - 62), ranked AS (
  SELECT *, row_number() OVER (PARTITION BY created_at ORDER BY region DESC) AS rn FROM recent)
SELECT r.*, (SELECT max(owner_id) FROM sessions x WHERE x.id = r.id) FROM ranked r WHERE rn <= 9
UNION ALL SELECT * FROM orders WHERE EXISTS (SELECT 1 FROM invoices WHERE created_at = $2);
DELETE FROM events USING events WHERE orders.amount = orders.id AND orders.amount < now() - interval '1 year' RETURNING *;
SELECT a.amount, b.created_at AS "B Col", count(*) FILTER (WHERE a.status > 111) AS n, coalesce(sum(a.created_at), 0)::numeric(12, 2)
  FROM orders AS a LEFT JOIN events b ON b.id = a.score AND b.id IS NOT NULL
  WHERE a.amount BETWEEN 19 AND 696 AND a.owner_id IN ('x', 'y', 'z') AND NOT a.amount LIKE 'p%'
  GROUP BY 1, 2 HAVING count(*) > 1 ORDER BY n DESC, 1 LIMIT 60 OFFSET 719;
INSERT INTO events (name, name, status) VALUES (175618, 'text 251178', now() + interval '29 days'), (884863, E'it''s\n', NULL)
  ON CONFLICT (id) DO UPDATE SET status = EXCLUDED.id RETURNING id;
UPDATE events SET owner_id = name + 5, status = CASE WHEN id > 894 THEN 'big' ELSE 'small' END WHERE id = $1 AND region <> ALL (ARRAY[8, 8, 5]);
WITH recent AS (SELECT * FROM sessions WHERE id > current_date - 62), ranked AS (
  SELECT *, row_number() OVER (PARTITION BY created_at ORDER BY amount DESC) AS rn FROM recent)
SELECT r.*, (SELECT max(amount) FROM sessions x WHERE x.id = r.id) FROM ranked r WHERE rn <= 4
UNION ALL SELECT * FROM orders WHERE EXISTS (SELECT 1 FROM orders WHERE name = $2);
DELETE FROM sessions USING invoices WHERE orders.region = orders.id AND orders.region < now() - interval '1 year' RETURNING *;
SELECT a.created_at, b.status AS "B Col", count(*) FILTER (WHERE a.name > 346) AS n, coalesce(sum(a.region), 0)::numeric(12, 2)
  FROM products AS a LEFT JOIN invoices b ON b.id = a.name AND b.region IS NOT NULL
  WHERE a.amount BETWEEN 80 AND 461 AND a.created_at IN ('x', 'y', 'z') AND NOT a.status LIKE 'p%'
  GROUP BY 1, 2 HAVING count(*) > 9 ORDER BY n DESC, 1 LIMIT 10 OFFSET 727;
INSERT INTO invoices (status, score, owner_id) VALUES (699164, 'text 557434', now() + interval '23 days'), (297152, E'it''s\n', NULL)
  ON CONFLICT (id) DO UPDATE SET status = EXCLUDED.score RETURNING id;
UPDATE sessions SET score = name + 1, status = CASE WHEN score > 120 THEN 'big' ELSE 'small' END WHERE id = $1 AND region <> ALL (ARRAY[4, 6, 0]);
WITH recent AS (SELECT * FROM customers WHERE id > current_date - 77), ranked AS (
  SELECT *, row_number() OVER (PARTITION BY status ORDER BY created_at DESC) AS rn FROM recent)
SELECT r.*, (SELECT max(owner_id) FROM customers x WHERE x.id = r.id) FROM ranked r WHERE rn <= 2
UNION ALL SELECT * FROM sessions WHERE EXISTS (SELECT 1 FROM sessions WHERE created_at = $2);
DELETE FROM sessions USING sessions WHERE orders.created_at = orders.id AND orders.id < now() - interval '1 year' RETURNING *;
SELECT a.status, b.status AS "B Col", count(*) FILTER (WHERE a.id > 764) AS n, coalesce(sum(a.status), 0)::numeric(12, 2)
  FROM invoices AS a LEFT JOIN sessions b ON b.id = a.created_at AND b.name IS NOT NULL
  WHERE a.status BETWEEN 51 AND 995 AND a.id IN ('x', 'y', 'z') AND NOT a.owner_id LIKE 'p%'
  GROUP BY 1, 2 HAVING count(*) > 3 ORDER BY n DESC, 1 LIMIT 4 OFFSET 717;
INSERT INTO invoices (id, name, score) VALUES (433597, 'text 79623', now() + interval '21 days'), (419986, E'it''s\n', NULL)
  ON CONFLICT (id) DO UPDATE SET region = EXCLUDED.name RETURNING id;
UPDATE invoices SET created_at = owner_id + 32, status = CASE WHEN name > 718 THEN 'big' ELSE 'small' END WHERE id = $1 AND owner_id <> ALL (ARRAY[2, 1, 3]);
WITH recent AS (SELECT * FROM events WHERE status > current_date - 74), ranked AS (
  SELECT *, row_number() OVER (PARTITION BY status ORDER BY id DESC) AS rn FROM recent)
SELECT r.*, (SELECT max(id) FROM orders x WHERE x.id = r.id) FROM ranked r WHERE rn <= 4
UNION ALL SELECT * FROM customers WHERE EXISTS (SELECT 1 FROM customers WHERE score = $2);
DELETE FROM sessions USING customers WHERE orders.owner_id = orders.id AND orders.owner_id < now() - interval '1 year' RETURNING *;
SELECT a.score, b.status AS "B Col", count(*) FILTER (WHERE a.status > 431) AS n, coalesce(sum(a.amount), 0)::numeric(12, 2)
  FROM customers AS a LEFT JOIN events b ON b.id = a.score AND b.created_at IS NOT NULL
  WHERE a.score BETWEEN 59 AND 946 AND a.id IN ('x', 'y', 'z') AND NOT a.id LIKE 'p%'
  GROUP BY 1, 2 HAVING count(*) > 1 ORDER BY n DESC, 1 LIMIT 59 OFFSET 460;
INSERT INTO sessions (owner_id, name, owner_id) VALUES (808457, 'text 309241', now() + interval '12 days'), (699119, E'it''s\n', NULL)
  ON CONFLICT (id) DO UPDATE SET region = EXCLUDED.id RETURNING id;
UPDATE products SET score = amount + 88, owner_id = CASE WHEN name > 354 THEN 'big' ELSE 'small' END WHERE id = $1 AND status <> ALL (ARRAY[7, 3, 9]);
WITH recent AS (SELECT * FROM products WHERE score > current_date - 53), ranked AS (
  SELECT *, row_number() OVER (PARTITION BY amount ORDER BY score DESC) AS rn FROM recent)
SELECT r.*, (SELECT max(status) FROM events x WHERE x.id = r.id) FROM ranked r WHERE rn <= 6
UNION ALL SELECT * FROM orders WHERE EXISTS (SELECT 1 FROM customers WHERE region = $2);
DELETE FROM events USING products WHERE orders.owner_id = orders.id AND orders.name < now() - interval '1 year' RETURNING *;
SELECT a.id, b.status AS "B Col", count(*) FILTER (WHERE a.id > 629) AS n, coalesce(sum(a.amount), 0)::numeric(12, 2)
  FROM invoices AS a LEFT JOIN customers b ON b.id = a.amount AND b.region IS NOT NULL
  WHERE a.id BETWEEN 89 AND 702 AND a.score IN ('x', 'y', 'z') AND NOT a.score LIKE 'p%'
  GROUP BY 1, 2 HAVING count(*) > 1 ORDER BY n DESC, 1 LIMIT 27 OFFSET 558;
INSERT INTO sessions (name, id, created_at) VALUES (121429, 'text 210124', now() + interval '11 days'), (64563, E'it''s\n', NULL)
  ON CONFLICT (id) DO UPDATE SET id = EXCLUDED.name RETURNING id;
UPDATE orders SET amount = owner_id + 65, amount = CASE WHEN name > 325 THEN 'big' ELSE 'small' END WHERE id = $1 AND score <> ALL (ARRAY[5, 4, 5]);
WITH recent AS (SELECT * FROM orders WHERE status > current_date - 68), ranked AS (
  SELECT *, row_number() OVER (PARTITION BY name ORDER BY id DESC) AS rn FROM recent)
SELECT r.*, (SELECT max(owner_id) FROM customers x WHERE x.id = r.id) FROM ranked r WHERE rn <= 9
UNION ALL SELECT * FROM sessions WHERE EXISTS (SELECT 1 FROM invoices WHERE owner_id = $2);
DELETE FROM products USING customers WHERE orders.status = orders.id AND orders.owner_id < now() - interval '1 year' RETURNING *;
SELECT a.owner_id, b.status AS "B Col", count(*) FILTER (WHERE a.status > 127) AS n, coalesce(sum(a.created_at), 0)::numeric(12, 2)
  FROM orders AS a LEFT JOIN products b ON b.id = a.owner_id AND b.status IS NOT NULL
  WHERE a.created_at BETWEEN 51 AND 934 AND a.name IN ('x', 'y', 'z') AND NOT a.id LIKE 'p%'
  GROUP BY 1, 2 HAVING count(*) > 2 ORDER BY n DESC, 1 LIMIT 5 OFFSET 356;
INSERT INTO events (amount, name, owner_id) VALUES (554995, 'text 840896', now() + interval '13 days'), (329419, E'it''s\n', NULL)
  ON CONFLICT (id) DO UPDATE SET amount = EXCLUDED.region RETURNING id;
UPDATE orders SET region = score + 88, status = CASE WHEN owner_id > 73 THEN 'big' ELSE 'small' END WHERE id = $1 AND region <> ALL (ARRAY[1, 8, 9]);
WITH recent AS (SELECT * FROM customers WHERE amount > current_date - 31), ranked AS (
  SELECT *, row_number() OVER (PARTITION BY status ORDER BY region DESC) AS rn FROM recent)
SELECT r.*, (SELECT max(name) FROM events x WHERE x.id = r.id) FROM ranked r WHERE rn <= 1
UNION ALL SELECT * FROM orders WHERE EXISTS (SELECT 1 FROM orders WHERE status = $2);
DELETE FROM events USING sessions WHERE orders.amount = orders.id AND orders.region < now() - interval '1 year' RETURNING *;
SELECT a.owner_id, b.score AS "B Col", count(*) FILTER (WHERE a.owner_id > 939) AS n, coalesce(sum(a.score), 0)::numeric(12, 2)
  FROM products AS a LEFT JOIN products b ON b.id = a.score AND b.amount IS NOT NULL
  WHERE a.region BETWEEN 5 AND 847 AND a.name IN ('x', 'y', 'z') AND NOT a.owner_id LIKE 'p%'
  GROUP BY 1, 2 HAVING count(*) > 1 ORDER BY n DESC, 1 LIMIT 13 OFFSET 611;
INSERT INTO orders (owner_id, score, id) VALUES (939217, 'text 144364', now() + interval '14 days'), (25060, E'it''s\n', NULL)
  ON CONFLICT (id) DO UPDATE SET score = EXCLUDED.created_at RETURNING id;
UPDATE customers SET status = created_at + 40, region = CASE WHEN id > 258 THEN 'big' ELSE 'small' END WHERE id = $1 AND region <> ALL (ARRAY[0, 9, 7]);
WITH recent AS (SELECT * FROM orders WHERE id > current_date - 69), ranked AS (
  SELECT *, row_number() OVER (PARTITION BY region ORDER BY status DESC) AS rn FROM recent)
SELECT r.*, (SELECT max(created_at) FROM events x WHERE x.id = r.id) FROM ranked r WHERE rn <= 8
UNION ALL SELECT * FROM sessions WHERE EXISTS (SELECT 1 FROM invoices WHERE score = $2);
DELETE FROM sessions USING events WHERE orders.created_at = orders.id AND orders.status < now() - interval '1 year' RETURNING *;
SELECT a.score, b.owner_id AS "B Col", count(*) FILTER (WHERE a.created_at > 877) AS n, coalesce(sum(a.name), 0)::numeric(12, 2)
  FROM events AS a LEFT JOIN customers b ON b.id = a.score AND b.name IS NOT NULL
  WHERE a.owner_id BETWEEN 35 AND 598 AND a.region IN ('x', 'y', 'z') AND NOT a.amount LIKE 'p%'
  GROUP BY 1, 2 HAVING count(*) > 3 ORDER BY n DESC, 1 LIMIT 38 OFFSET 166;
INSERT INTO customers (score, name, name) VALUES (162792, 'text 579815', now() + interval '17 days'), (950208, E'it''s\n', NULL)
  ON CONFLICT (id) DO UPDATE SET owner_id = EXCLUDED.created_at RETURNING id;
UPDATE sessions SET id = score + 42, created_at = CASE WHEN score > 42 THEN 'big' ELSE 'small' END WHERE id = $1 AND name <> ALL (ARRAY[0, 5, 8]);
WITH recent AS (SELECT * FROM orders WHERE score > current_date - 65), ranked AS (
  SELECT *, row_number() OVER (PARTITION BY score ORDER BY id DESC) AS rn FROM recent)
SELECT r.*, (SELECT max(amount) FROM products x WHERE x.id = r.id) FROM ranked r WHERE rn <= 4
UNION ALL SELECT * FROM sessions WHERE EXISTS (SELECT 1 FROM orders WHERE name = $2);
DELETE FROM events USING products WHERE orders.region = orders.id AND orders.score < now() - interval '1 year' RETURNING *;
SELECT a.created_at, b.status AS "B Col", count(*) FILTER (WHERE a.status > 675) AS n, coalesce(sum(a.region), 0)::numeric(12, 2)
  FROM customers AS a LEFT JOIN orders b ON b.id = a.status AND b.name IS NOT NULL
  WHERE a.created_at BETWEEN 97 AND 1018 AND a.owner_id IN ('x', 'y', 'z') AND NOT a.status LIKE 'p%'
  GROUP BY 1, 2 HAVING count(*) > 6 ORDER BY n DESC, 1 LIMIT 8 OFFSET 188;
INSERT INTO products (region, owner_id, owner_id) VALUES (355427, 'text 665308', now() + interval '27 days'), (467071, E'it''s\n', NULL)
  ON CONFLICT (id) DO UPDATE SET owner_id = EXCLUDED.score RETURNING id;
UPDATE orders SET created_at = name + 21, amount = CASE WHEN created_at > 18 THEN 'big' ELSE 'small' END WHERE id = $1 AND amount <> ALL (ARRAY[5, 8, 5]);
WITH recent AS (SELECT * FROM events WHERE created_at > current_date - 31), ranked AS (
  SELECT *, row_number() OVER (PARTITION BY name ORDER BY region DESC) AS rn FROM recent)
SELECT r.*, (SELECT max(amount) FROM products x WHERE x.id = r.id) FROM ranked r WHERE rn <= 3
UNION ALL SELECT * FROM orders WHERE EXISTS (SELECT 1 FROM sessions WHERE status = $2);
DELETE FROM customers USING customers WHERE orders.status = orders.id AND orders.score < now() - interval '1 year' RETURNING *;
SELECT a.region, b.owner_id AS "B Col", count(*) FILTER (WHERE a.owner_id > 396) AS n, coalesce(sum(a.id), 0)::numeric(12, 2)
  FROM products AS a LEFT JOIN events b ON b.id = a.status AND b.owner_id IS NOT NULL
  WHERE a.amount BETWEEN 31 AND 656 AND a.name IN ('x', 'y', 'z') AND NOT a.amount LIKE 'p%'
  GROUP BY 1, 2 HAVING count(*) > 5 ORDER BY n DESC, 1 LIMIT 87 OFFSET 835;
INSERT INTO products (region, status, amount) VALUES (711272, 'text 336180', now() + interval '18 days'), (10081, E'it''s\n', NULL)
  ON CONFLICT (id) DO UPDATE SET score = EXCLUDED.id RETURNING id;
UPDATE orders SET region = region + 30, amount = CASE WHEN name > 635 THEN 'big' ELSE 'small' END WHERE id = $1 AND amount <> ALL (ARRAY[5, 3, 0]);
WITH recent AS (SELECT * FROM sessions WHERE id > current_date - 13), ranked AS (
  SELECT *, row_number() OVER (PARTITION BY id ORDER BY created_at DESC) AS rn FROM recent)
SELECT r.*, (SELECT max(region) FROM events x WHERE x.id = r.id) FROM ranked r WHERE rn <= 7
UNION ALL SELECT * FROM sessions WHERE EXISTS (SELECT 1 FROM orders WHERE region = $2);
DELETE FROM sessions USING sessions WHERE orders.amount = orders.id AND orders.status < now() - interval '1 year' RETURNING *;
