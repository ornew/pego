-- Known deviations of group D (the reference accepts, the grammar rejects). Not run by pgcases.sh (copy to cases/ to run).
-- XMLTABLE column option name: a unicode escaped identifier that decodes to default or path
select * from xmltable('/r' passing x columns a int U&"d\0065fault" 1);
-- the encoding of FORMAT JSON ENCODING: a unicode escaped identifier that decodes to utf8
select json_object(returning bytea format json encoding U&"utf\0038");
