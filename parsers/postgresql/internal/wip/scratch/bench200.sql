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
