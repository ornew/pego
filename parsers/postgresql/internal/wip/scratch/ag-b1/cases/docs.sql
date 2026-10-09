-- create_function
RETURN expression
-- create_function
BEGIN ATOMIC
  statement;
  statement;
  ...
  statement;
END
-- create_function
CREATE FUNCTION foo(int) ...
CREATE FUNCTION foo(int, out text) ...
-- create_function
CREATE FUNCTION foo(int) ...
CREATE FUNCTION foo(int, int default 42) ...
-- create_function
CREATE FUNCTION add(integer, integer) RETURNS integer
    AS 'select $1 + $2;'
    LANGUAGE SQL
    IMMUTABLE
    RETURNS NULL ON NULL INPUT;
-- create_function
CREATE FUNCTION add(a integer, b integer) RETURNS integer
    LANGUAGE SQL
    IMMUTABLE
    RETURNS NULL ON NULL INPUT
    RETURN a + b;
-- create_function
CREATE OR REPLACE FUNCTION increment(i integer) RETURNS integer AS $$
        BEGIN
                RETURN i + 1;
        END;
$$ LANGUAGE plpgsql;
-- create_function
CREATE FUNCTION dup(in int, out f1 int, out f2 text)
    AS $$ SELECT $1, CAST($1 AS text) || ' is text' $$
    LANGUAGE SQL;

SELECT * FROM dup(42);
-- create_function
CREATE TYPE dup_result AS (f1 int, f2 text);

CREATE FUNCTION dup(int) RETURNS dup_result
    AS $$ SELECT $1, CAST($1 AS text) || ' is text' $$
    LANGUAGE SQL;

SELECT * FROM dup(42);
-- create_function
CREATE FUNCTION dup(int) RETURNS TABLE(f1 int, f2 text)
    AS $$ SELECT $1, CAST($1 AS text) || ' is text' $$
    LANGUAGE SQL;

SELECT * FROM dup(42);
-- create_function
CREATE FUNCTION check_password(uname TEXT, pass TEXT)
RETURNS BOOLEAN AS $$
DECLARE passed BOOLEAN;
BEGIN
        SELECT  (pwd = $2) INTO passed
        FROM    pwds
        WHERE   username = $1;

        RETURN passed;
END;
$$  LANGUAGE plpgsql
    SECURITY DEFINER
    -- Set a secure search_path: trusted schema(s), then 'pg_temp'.
    SET search_path = admin, pg_temp;
-- create_function
BEGIN;
CREATE FUNCTION check_password(uname TEXT, pass TEXT) ... SECURITY DEFINER;
REVOKE ALL ON FUNCTION check_password(uname TEXT, pass TEXT) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION check_password(uname TEXT, pass TEXT) TO admins;
COMMIT;
-- create_procedure
BEGIN ATOMIC
  statement;
  statement;
  ...
  statement;
END
-- create_procedure
CREATE PROCEDURE insert_data(a integer, b integer)
LANGUAGE SQL
AS $$
INSERT INTO tbl VALUES (a);
INSERT INTO tbl VALUES (b);
$$;
-- create_procedure
CREATE PROCEDURE insert_data(a integer, b integer)
LANGUAGE SQL
BEGIN ATOMIC
  INSERT INTO tbl VALUES (a);
  INSERT INTO tbl VALUES (b);
END;
-- create_procedure
CALL insert_data(1, 2);
-- create_cast
SELECT CAST(42 AS float8);
-- create_cast
INSERT INTO foo (f1) VALUES (42);
-- create_cast
SELECT 2 + 4.0;
-- create_cast
SELECT CAST ( 2 AS numeric ) + 4.0;
-- create_cast
CREATE CAST (bigint AS int4) WITH FUNCTION int4(bigint) AS ASSIGNMENT;
-- create_transform
CREATE TYPE hstore ...;

CREATE EXTENSION plpython3u;
-- create_transform
CREATE FUNCTION hstore_to_plpython(val internal) RETURNS internal
LANGUAGE C STRICT IMMUTABLE
AS ...;

CREATE FUNCTION plpython_to_hstore(val internal) RETURNS hstore
LANGUAGE C STRICT IMMUTABLE
AS ...;
-- create_transform
CREATE TRANSFORM FOR hstore LANGUAGE plpython3u (
    FROM SQL WITH FUNCTION hstore_to_plpython(internal),
    TO SQL WITH FUNCTION plpython_to_hstore(internal)
);
-- create_aggregate
sfunc( internal-state, next-data-values ) ---> next-internal-state
ffunc( internal-state ) ---> aggregate-value
-- create_aggregate
SELECT agg(col) FROM tab;
-- create_aggregate
SELECT col FROM tab ORDER BY col USING sortop LIMIT 1;
-- create_operator
COMMUTATOR = OPERATOR(myschema.===) ,
-- create_operator
CREATE OPERATOR === (
    LEFTARG = box,
    RIGHTARG = box,
    FUNCTION = area_equal_function,
    COMMUTATOR = ===,
    NEGATOR = !==,
    RESTRICT = area_restriction_function,
    JOIN = area_join_function,
    HASHES, MERGES
);
-- create_opclass
CREATE OPERATOR CLASS gist__int_ops
    DEFAULT FOR TYPE _int4 USING gist AS
        OPERATOR        3       &&,
        OPERATOR        6       = (anyarray, anyarray),
        OPERATOR        7       @>,
        OPERATOR        8       <@,
        OPERATOR        20      @@ (_int4, query_int),
        FUNCTION        1       g_int_consistent (internal, _int4, smallint, oid, internal),
        FUNCTION        2       g_int_union (internal, internal),
        FUNCTION        3       g_int_compress (internal),
        FUNCTION        4       g_int_decompress (internal),
        FUNCTION        5       g_int_penalty (internal, internal, internal),
        FUNCTION        6       g_int_picksplit (internal, internal),
        FUNCTION        7       g_int_same (_int4, _int4, internal);
-- create_type
CREATE TYPE compfoo AS (f1 int, f2 text);

CREATE FUNCTION getfoo() RETURNS SETOF compfoo AS $$
    SELECT fooid, fooname FROM foo
$$ LANGUAGE SQL;
-- create_type
CREATE TYPE bug_status AS ENUM ('new', 'open', 'closed');

CREATE TABLE bug (
    id serial,
    description text,
    status bug_status
);
-- create_type
CREATE TYPE float8_range AS RANGE (subtype = float8, subtype_diff = float8mi);
-- create_type
CREATE TYPE box;

CREATE FUNCTION my_box_in_function(cstring) RETURNS box AS ... ;
CREATE FUNCTION my_box_out_function(box) RETURNS cstring AS ... ;

CREATE TYPE box (
    INTERNALLENGTH = 16,
    INPUT = my_box_in_function,
    OUTPUT = my_box_out_function
);

CREATE TABLE myboxes (
    id integer,
    description box
);
-- create_type
CREATE TYPE box (
    INTERNALLENGTH = 16,
    INPUT = my_box_in_function,
    OUTPUT = my_box_out_function,
    ELEMENT = float4
);
-- create_type
CREATE TYPE bigobj (
    INPUT = lo_filein, OUTPUT = lo_fileout,
    INTERNALLENGTH = VARIABLE
);
CREATE TABLE big_objs (
    id integer,
    obj bigobj
);
-- create_tsdictionary
CREATE TEXT SEARCH DICTIONARY my_russian (
    template = snowball,
    language = russian,
    stopwords = myrussian
);
-- create_collation
CREATE COLLATION french (locale = 'fr_FR.utf8');
-- create_collation
CREATE COLLATION german_phonebook (provider = icu, locale = 'de-u-co-phonebk');
-- create_collation

-- create_collation
CREATE COLLATION german FROM "de_DE";
-- create_conversion
conv_proc(
    integer,  -- source encoding ID
    integer,  -- destination encoding ID
    cstring,  -- source string (null terminated C string)
    internal, -- destination (fill with a null terminated C string)
    integer,  -- source string length
    boolean   -- if true, don't throw an error if conversion fails
) RETURNS integer;
-- create_conversion
CREATE CONVERSION myconv FOR 'UTF8' TO 'LATIN1' FROM myfunc;
-- create_language
CREATE FUNCTION plsample_call_handler() RETURNS language_handler
    AS '$libdir/plsample'
    LANGUAGE C;
CREATE LANGUAGE plsample
    HANDLER plsample_call_handler;
-- create_language
CREATE EXTENSION plsample;
-- create_extension
CREATE EXTENSION hstore SCHEMA addons;
-- create_extension
SET search_path = addons;
CREATE EXTENSION hstore;
-- create_access_method
CREATE ACCESS METHOD heptree TYPE INDEX HANDLER heptree_handler;
-- alter_function
ALTER FUNCTION sqrt(integer) RENAME TO square_root;
-- alter_function
ALTER FUNCTION sqrt(integer) OWNER TO joe;
-- alter_function
ALTER FUNCTION sqrt(integer) SET SCHEMA maths;
-- alter_function
ALTER FUNCTION sqrt(integer) DEPENDS ON EXTENSION mathlib;
-- alter_function
ALTER FUNCTION check_password(text) SET search_path = admin, pg_temp;
-- alter_function
ALTER FUNCTION check_password(text) RESET search_path;
-- alter_procedure
ALTER PROCEDURE insert_data(integer, integer) RENAME TO insert_record;
-- alter_procedure
ALTER PROCEDURE insert_data(integer, integer) OWNER TO joe;
-- alter_procedure
ALTER PROCEDURE insert_data(integer, integer) SET SCHEMA accounting;
-- alter_procedure
ALTER PROCEDURE insert_data(integer, integer) DEPENDS ON EXTENSION myext;
-- alter_procedure
ALTER PROCEDURE check_password(text) SET search_path = admin, pg_temp;
-- alter_procedure
ALTER PROCEDURE check_password(text) RESET search_path;
-- alter_routine
ALTER ROUTINE foo(integer) RENAME TO foobar;
-- alter_type
ALTER TYPE electronic_mail RENAME TO email;
-- alter_type
ALTER TYPE email OWNER TO joe;
-- alter_type
ALTER TYPE email SET SCHEMA customers;
-- alter_type
ALTER TYPE compfoo ADD ATTRIBUTE f3 int;
-- alter_type
ALTER TYPE colors ADD VALUE 'orange' AFTER 'red';
-- alter_type
ALTER TYPE colors RENAME VALUE 'purple' TO 'mauve';
-- alter_type
CREATE FUNCTION mytypesend(mytype) RETURNS bytea ...;
CREATE FUNCTION mytyperecv(internal, oid, integer) RETURNS mytype ...;
ALTER TYPE mytype SET (
    SEND = mytypesend,
    RECEIVE = mytyperecv
);
-- alter_operator
ALTER OPERATOR @@ (text, text) OWNER TO joe;
-- alter_operator
ALTER OPERATOR && (int[], int[]) SET (RESTRICT = _int_contsel, JOIN = _int_contjoinsel);
-- alter_operator
ALTER OPERATOR && (int[], int[]) SET (COMMUTATOR = &&);
-- alter_opfamily
ALTER OPERATOR FAMILY integer_ops USING btree ADD

  -- int4 vs int2
  OPERATOR 1 < (int4, int2) ,
  OPERATOR 2 <= (int4, int2) ,
  OPERATOR 3 = (int4, int2) ,
  OPERATOR 4 >= (int4, int2) ,
  OPERATOR 5 > (int4, int2) ,
  FUNCTION 1 btint42cmp(int4, int2) ,

  -- int2 vs int4
  OPERATOR 1 < (int2, int4) ,
  OPERATOR 2 <= (int2, int4) ,
  OPERATOR 3 = (int2, int4) ,
  OPERATOR 4 >= (int2, int4) ,
  OPERATOR 5 > (int2, int4) ,
  FUNCTION 1 btint24cmp(int2, int4) ;
-- alter_opfamily
ALTER OPERATOR FAMILY integer_ops USING btree DROP

  -- int4 vs int2
  OPERATOR 1 (int4, int2) ,
  OPERATOR 2 (int4, int2) ,
  OPERATOR 3 (int4, int2) ,
  OPERATOR 4 (int4, int2) ,
  OPERATOR 5 (int4, int2) ,
  FUNCTION 1 (int4, int2) ,

  -- int2 vs int4
  OPERATOR 1 (int2, int4) ,
  OPERATOR 2 (int2, int4) ,
  OPERATOR 3 (int2, int4) ,
  OPERATOR 4 (int2, int4) ,
  OPERATOR 5 (int2, int4) ,
  FUNCTION 1 (int2, int4) ;
-- alter_tsconfig
ALTER TEXT SEARCH CONFIGURATION my_config
  ALTER MAPPING REPLACE english WITH swedish;
-- alter_tsdictionary
ALTER TEXT SEARCH DICTIONARY my_dict ( StopWords = newrussian );
-- alter_tsdictionary
ALTER TEXT SEARCH DICTIONARY my_dict ( language = dutch, StopWords );
-- alter_tsdictionary
ALTER TEXT SEARCH DICTIONARY my_dict ( dummy );
-- alter_collation
 pg_collation_actual_version(c.oid)
  ORDER BY 1, 2;
]]>
-- alter_collation
ALTER COLLATION "de_DE" RENAME TO german;
-- alter_collation
ALTER COLLATION "en_US" OWNER TO joe;
-- alter_extension
ALTER EXTENSION hstore UPDATE TO '2.0';
-- alter_extension
ALTER EXTENSION hstore SET SCHEMA utils;
-- alter_extension
ALTER EXTENSION hstore ADD FUNCTION populate_record(anyelement, hstore);
-- drop_cast
DROP CAST (text AS int);
-- drop_transform
DROP TRANSFORM FOR hstore LANGUAGE plpython3u;
-- drop_opclass
DROP OPERATOR CLASS widget_ops USING btree;
-- drop_opfamily
DROP OPERATOR FAMILY float_ops USING btree;
-- drop_function
DROP FUNCTION sqrt(integer);
-- drop_function
DROP FUNCTION sqrt(integer), sqrt(bigint);
-- drop_function
DROP FUNCTION update_employee_salaries;
-- drop_function
DROP FUNCTION update_employee_salaries();
-- drop_aggregate
DROP AGGREGATE myavg(integer);
-- drop_aggregate
DROP AGGREGATE myrank(VARIADIC "any" ORDER BY VARIADIC "any");
-- drop_aggregate
DROP AGGREGATE myavg(integer), myavg(bigint);
-- drop_operator
DROP OPERATOR ^ (integer, integer);
-- drop_operator
DROP OPERATOR ~ (none, bit);
-- drop_operator
DROP OPERATOR ~ (none, bit), ^ (integer, integer);
