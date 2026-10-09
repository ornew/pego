-- Transactions and COPY (group C1)

BEGIN;
BEGIN WORK;
BEGIN TRANSACTION;
BEGIN ISOLATION LEVEL SERIALIZABLE;
BEGIN TRANSACTION ISOLATION LEVEL READ COMMITTED, READ ONLY;
BEGIN WORK READ WRITE DEFERRABLE;
BEGIN WORK NOT DEFERRABLE ISOLATION LEVEL REPEATABLE READ READ ONLY;
BEGIN ISOLATION LEVEL READ UNCOMMITTED;
BEGIN READ ONLY, READ WRITE;
begin work;
BEGIN AND CHAIN;
BEGIN WORK AND CHAIN;
BEGIN READ;
BEGIN ISOLATION LEVEL;
BEGIN,;
BEGIN READ ONLY,;
BEGIN TRANSACTION WORK;
START TRANSACTION;
START TRANSACTION READ ONLY;
START TRANSACTION ISOLATION LEVEL SERIALIZABLE, READ WRITE, NOT DEFERRABLE;
START TRANSACTION READ ONLY DEFERRABLE;
start transaction isolation level repeatable read;
START;
START WORK;
START TRANSACTION AND CHAIN;
START TRANSACTION,;
COMMIT;
COMMIT WORK;
COMMIT TRANSACTION;
COMMIT AND CHAIN;
COMMIT AND NO CHAIN;
COMMIT WORK AND CHAIN;
COMMIT TRANSACTION AND NO CHAIN;
COMMIT AND;
COMMIT NO CHAIN;
COMMIT WORK TRANSACTION;
COMMIT PREPARED 'gid';
COMMIT PREPARED 'a' 'b';
COMMIT PREPARED x;
COMMIT PREPARED;
COMMIT WORK PREPARED 'x';
commit prepared E'g\x41';
END;
END WORK;
END TRANSACTION;
END AND CHAIN;
END WORK AND NO CHAIN;
end transaction and chain;
END PREPARED 'x';
ABORT;
ABORT WORK;
ABORT TRANSACTION;
ABORT AND CHAIN;
ABORT TRANSACTION AND NO CHAIN;
ABORT PREPARED 'x';
ROLLBACK;
ROLLBACK WORK;
ROLLBACK TRANSACTION;
ROLLBACK AND CHAIN;
ROLLBACK AND NO CHAIN;
ROLLBACK TRANSACTION AND CHAIN;
ROLLBACK PREPARED 'gid';
ROLLBACK PREPARED x;
ROLLBACK TO sp;
ROLLBACK TO SAVEPOINT sp;
ROLLBACK WORK TO sp;
ROLLBACK WORK TO SAVEPOINT sp;
ROLLBACK TRANSACTION TO sp;
ROLLBACK TRANSACTION TO SAVEPOINT "Sp";
ROLLBACK TO savepoint;
ROLLBACK TO SAVEPOINT savepoint;
ROLLBACK TO SAVEPOINT;
ROLLBACK TO;
ROLLBACK TO SAVEPOINT a b;
ROLLBACK TO user;
ROLLBACK TO work;
ROLLBACK TO a.b;
ROLLBACK AND CHAIN TO sp;
ROLLBACK TO sp AND CHAIN;
SAVEPOINT sp;
SAVEPOINT "Sp";
SAVEPOINT savepoint;
SAVEPOINT work;
SAVEPOINT;
SAVEPOINT user;
SAVEPOINT a, b;
SAVEPOINT a.b;
savepoint SP;
RELEASE sp;
RELEASE SAVEPOINT sp;
RELEASE savepoint;
RELEASE SAVEPOINT savepoint;
RELEASE SAVEPOINT;
RELEASE;
RELEASE a.b;
RELEASE user;
PREPARE TRANSACTION 'gid';
PREPARE TRANSACTION x;
PREPARE TRANSACTION;
PREPARE TRANSACTION 'a' 'b';
PREPARE TRANSACTION AS SELECT 1;
PREPARE transaction AS SELECT 1;
PREPARE transaction (int) AS SELECT $1;
PREPARE TRANSACTION E'g';
-- comments
BEGIN /* a */ WORK /* b */ ISOLATION -- c
 LEVEL /* d */ SERIALIZABLE;
COMMIT -- x
 AND /* y */ CHAIN;

-- COPY: table forms
COPY t FROM STDIN;
\.
COPY t TO STDOUT;
COPY t FROM 'file';
COPY t TO 'file';
COPY t FROM 'a' 'b';
COPY t FROM E'a\\b';
COPY s.t FROM stdin;
\.
COPY c.s.t FROM stdin;
\.
COPY "T" FROM stdin;
\.
COPY t (a) FROM stdin;
\.
COPY t (a, b, "C") FROM stdin;
\.
COPY t () FROM stdin;
\.
COPY t FROM PROGRAM 'cmd';
COPY t TO PROGRAM 'cmd';
COPY t FROM PROGRAM STDIN;
\.
COPY t TO PROGRAM STDOUT;
COPY t FROM PROGRAM;
COPY t FROM program 'x';
COPY BINARY t FROM stdin;
\.
COPY BINARY t (a) TO 'f';
COPY binary t TO stdout;
COPY t FROM STDOUT;
COPY t TO STDIN;
\.
COPY t FROM;
COPY t STDIN;
\.
COPY FROM stdin;
\.
COPY t;
COPY;
COPY t FROM stdin WHERE a > 1;
\.
COPY t (a) FROM 'f' WHERE a > 1 AND b < 2;
COPY t FROM stdin WITH (format csv) WHERE a;
\.
COPY t FROM stdin CSV HEADER WHERE a;
\.
COPY t TO stdout WHERE a > 1;
COPY t TO 'f' WITH (format csv) WHERE a;
COPY t FROM stdin WHERE;
\.
-- old syntax: DELIMITERS and the items
COPY t FROM stdin DELIMITERS ',';
\.
COPY t FROM stdin USING DELIMITERS ',';
\.
COPY t FROM stdin USING DELIMITERS ',' WITH NULL AS 'x';
\.
COPY t FROM stdin WITH DELIMITERS ',';
\.
COPY t FROM stdin DELIMITERS;
\.
COPY t FROM stdin USING ',';
\.
COPY t FROM stdin USING DELIMITER ',';
\.
COPY t FROM stdin DELIMITERS ',' DELIMITER ';';
\.
COPY t FROM stdin WITH BINARY;
\.
COPY t FROM stdin BINARY;
\.
COPY BINARY t FROM stdin BINARY;
\.
COPY t FROM stdin FREEZE;
\.
COPY t FROM stdin WITH FREEZE;
\.
COPY t FROM stdin DELIMITER ',';
\.
COPY t FROM stdin DELIMITER AS ',';
\.
COPY t FROM stdin WITH DELIMITER AS ',';
\.
COPY t FROM stdin DELIMITER;
\.
COPY t FROM stdin NULL 'x';
\.
COPY t FROM stdin NULL AS 'x';
\.
COPY t FROM stdin NULL AS;
\.
COPY t FROM stdin NULL x;
\.
COPY t FROM stdin CSV;
\.
COPY t FROM stdin WITH CSV;
\.
COPY t FROM stdin CSV HEADER;
\.
COPY t FROM stdin HEADER;
\.
COPY t FROM stdin CSV HEADER QUOTE '"' ESCAPE '\';
\.
COPY t FROM stdin QUOTE AS '"';
\.
COPY t FROM stdin ESCAPE AS '\';
\.
COPY t FROM stdin QUOTE;
\.
COPY t FROM stdin ESCAPE;
\.
COPY t FROM stdin ENCODING 'utf8';
\.
COPY t FROM stdin ENCODING AS 'utf8';
\.
COPY t FROM stdin ENCODING;
\.
COPY t FROM stdin CSV FORCE QUOTE a;
\.
COPY t FROM stdin CSV FORCE QUOTE a, b;
\.
COPY t FROM stdin CSV FORCE QUOTE *;
\.
COPY t FROM stdin CSV FORCE NOT NULL a;
\.
COPY t FROM stdin CSV FORCE NOT NULL a, "B";
\.
COPY t FROM stdin CSV FORCE NOT NULL *;
\.
COPY t FROM stdin CSV FORCE NULL a;
\.
COPY t FROM stdin CSV FORCE NULL a, b;
\.
COPY t FROM stdin CSV FORCE NULL *;
\.
COPY t FROM stdin CSV FORCE QUOTE (a);
\.
COPY t FROM stdin CSV FORCE;
\.
COPY t FROM stdin CSV FORCE NOT;
\.
COPY t FROM stdin CSV FORCE QUOTE;
\.
COPY t FROM stdin CSV FORCE NOT NULL;
\.
COPY t FROM stdin CSV FORCE QUOTE a,;
\.
COPY t FROM stdin CSV FORCE QUOTE *, a;
\.
COPY t FROM stdin BINARY CSV FREEZE HEADER DELIMITER ',' NULL 'x' QUOTE '"' ESCAPE '\' FORCE QUOTE a ENCODING 'x';
\.
COPY t FROM stdin CSV, HEADER;
\.
COPY t FROM stdin WITH WITH CSV;
\.
COPY t FROM stdin foo;
\.
-- the generic options
COPY t FROM stdin (format csv);
\.
COPY t FROM stdin WITH (format csv);
\.
COPY t FROM stdin (format 'csv', header, delimiter ',');
\.
COPY t FROM stdin (format text, header true, header false, header on, header off);
\.
COPY t FROM stdin (header 1, header 0, header -1, header +1, header 1.5, header -2.5);
\.
COPY t FROM stdin (force_quote (a, b), force_not_null (c), force_null (d));
\.
COPY t FROM stdin (force_quote *);
\.
COPY t FROM stdin (force_quote (*));
\.
COPY t FROM stdin (force_quote ());
\.
COPY t FROM stdin (force_quote (a, 'b', true, on, off, "C"));
\.
COPY t FROM stdin (force_quote (a, 1));
\.
COPY t FROM stdin (force_quote (a b));
\.
COPY t FROM stdin (default_value default);
\.
COPY t FROM stdin (null 'x', default 'y');
\.
COPY t FROM stdin (freeze);
\.
COPY t FROM stdin (encoding 'utf8');
\.
COPY t FROM stdin (on_error ignore, log_verbosity verbose);
\.
COPY t FROM stdin (reject_limit 5);
\.
COPY t FROM stdin (select);
\.
COPY t FROM stdin (user);
\.
COPY t FROM stdin (any 1);
\.
COPY t FROM stdin (format csv,);
\.
COPY t FROM stdin (,format csv);
\.
COPY t FROM stdin ();
\.
COPY t FROM stdin (format csv header);
\.
COPY t FROM stdin (format csv) csv;
\.
COPY t FROM stdin csv (format csv);
\.
COPY t FROM stdin (a (b));
\.
COPY t FROM stdin (a (b, c), d *, e default);
\.
COPY t FROM stdin (a.b c);
\.
COPY t FROM stdin (a x y);
\.
COPY t FROM stdin (a = 1);
\.
COPY t FROM stdin (a 'x' 'y');
\.
COPY t FROM stdin (a (default));
\.
COPY t FROM stdin (a (1));
\.
COPY t TO stdout (format csv, header);
COPY t TO stdout (format json);
COPY t TO stdout (format JSON, header);
COPY t TO stdout (header, format /* c */ json);
COPY t TO stdout (format "json");
COPY t TO stdout (format 'json');
COPY t TO stdout (a format, b);
COPY t TO stdout (a format json);
COPY t TO stdout (format format);
COPY t TO stdout (format csv, format)
COPY t TO 'file' WITH (format binary);
-- COPY (query)
COPY (SELECT 1) TO stdout;
COPY (SELECT 1) TO 'f';
COPY (SELECT * FROM t WHERE a > 1) TO PROGRAM 'cmd';
COPY (SELECT 1) TO stdout WITH (format csv);
COPY (SELECT 1) TO stdout (format csv, header);
COPY (SELECT 1) TO stdout CSV HEADER;
COPY (SELECT 1) TO stdout WITH CSV;
COPY (INSERT INTO t VALUES (1) RETURNING *) TO stdout;
COPY (UPDATE t SET a = 1 RETURNING *) TO stdout;
COPY (DELETE FROM t RETURNING *) TO stdout;
COPY (MERGE INTO t USING s ON t.a = s.a WHEN MATCHED THEN DELETE RETURNING *) TO stdout;
COPY (VALUES (1)) TO stdout;
COPY (WITH x AS (SELECT 1) SELECT * FROM x) TO stdout;
COPY ((SELECT 1)) TO stdout;
COPY (SELECT 1) FROM stdin;
\.
COPY (SELECT 1) TO PROGRAM STDOUT;
COPY (SELECT 1) TO STDIN;
\.
COPY (SELECT 1) TO stdout DELIMITERS ',';
COPY (SELECT 1) TO stdout WHERE a;
COPY (SELECT 1) TO stdout BINARY;
COPY BINARY (SELECT 1) TO stdout;
COPY (SELECT 1) TO;
COPY (SELECT 1);
COPY (SELECT 1) TO stdout NULL 'x' CSV;
COPY (EXPLAIN SELECT 1) TO stdout;
COPY (CREATE TABLE x (a int)) TO stdout;
COPY () TO stdout;
COPY (SELECT 1) TO 'a' 'b';
COPY /* a */ t -- b
 FROM /* c */ stdin /* d */ WITH /* e */ (format /* f */ csv);
\.
