-- statements that need changes of the core (see the report): rows from, a function alias with a column definition list,
-- a parenthesized query with only LIMIT, a window function in the arguments of coalesce in FROM
select * from rows from (json_arrayagg(v), json_objectagg(k : v));
select * from rows from (json_arrayagg(v) as (a int));
select * from json_array(1) as t(a int);
select json_array((select 1) order by 1);
select json_array((select 1) limit 1);
select * from coalesce(sum(v) over ());
select * from rows from (f(json_arrayagg(v) filter (where x)));
