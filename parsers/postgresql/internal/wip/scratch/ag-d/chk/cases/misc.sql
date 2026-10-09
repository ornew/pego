-- keywords in different cases, comments between all tokens
SELECT JSON_OBJECT /* a */ ( /* b */ 'a' /* c */ : /* d */ 1 /* e */ NULL /* f */ ON /* g */ NULL /* h */ WITH /* i */ UNIQUE /* j */ KEYS /* k */ RETURNING /* l */ JSONB /* m */ FORMAT /* n */ JSON /* o */ ENCODING /* p */ UTF8 /* q */ ) -- r
;
Select Json_Object('a' : 1 Absent On Null Without Unique Keys Returning Json);
select json_array /* a */ ( /* b */ 1 /* c */ format /* d */ json /* e */ , /* f */ 2 /* g */ absent /* h */ on /* i */ null /* j */ returning /* k */ text /* l */ ) -- m
;
select JSON_ARRAY(SELECT /* a */ 1 /* b */ FORMAT /* c */ JSON /* d */ RETURNING /* e */ jsonb);
select json_query /* a */ ( /* b */ a /* c */ , /* d */ 'x' /* e */ passing /* f */ 1 /* g */ as /* h */ v /* i */ returning /* j */ int /* k */ with /* l */ conditional /* m */ array /* n */ wrapper /* o */ omit /* p */ quotes /* q */ on /* r */ scalar /* s */ string /* t */ empty /* u */ array /* v */ on /* w */ empty /* x */ error /* y */ on /* z */ error /* A */ ) -- B
;
select json_exists(a, 'x' passing 1 as v ERROR ON ERROR);
select json_value(a, 'x' RETURNING int NULL ON EMPTY DEFAULT 1 ON ERROR);
select * from JSON_TABLE /* a */ ( /* b */ x /* c */ , /* d */ '$' /* e */ as /* f */ p /* g */ passing /* h */ 1 /* i */ as /* j */ v /* k */ columns /* l */ ( /* m */ a /* n */ int /* o */ path /* p */ '$' /* q */ , /* r */ nested /* s */ path /* t */ '$' /* u */ columns /* v */ ( /* w */ b /* x */ for /* y */ ordinality /* z */ ) /* A */ ) /* B */ error /* C */ on /* D */ error /* E */ ) /* F */ as /* G */ j /* H */ ( /* I */ c /* J */ )
;
select * from XMLTABLE /* a */ ( /* b */ XMLNAMESPACES /* c */ ( /* d */ 'u' /* e */ as /* f */ a /* g */ , /* h */ default /* i */ 'd' /* j */ ) /* k */ , /* l */ '/r' /* m */ passing /* n */ by /* o */ ref /* p */ x /* q */ by /* r */ value /* s */ columns /* t */ a /* u */ int /* v */ path /* w */ 'p' /* x */ default /* y */ 1 /* z */ not /* A */ null /* B */ , /* C */ b /* D */ for /* E */ ordinality /* F */ ) /* G */ t /* H */
;
select XMLELEMENT /* a */ ( /* b */ NAME /* c */ foo /* d */ , /* e */ XMLATTRIBUTES /* f */ ( /* g */ a /* h */ AS /* i */ b /* j */ ) /* k */ , /* l */ c /* m */ ) /* n */
;
select XMLEXISTS ('//a' PASSING BY REF x BY VALUE);
select XMLPARSE ( DOCUMENT x PRESERVE WHITESPACE ), XMLSERIALIZE ( CONTENT x AS text NO INDENT ), XMLROOT ( x , VERSION NO VALUE , STANDALONE NO VALUE ), XMLPI ( NAME a , b ), XMLFOREST ( a AS b ), XMLCONCAT ( a , b );
select a IS /* a */ NOT /* b */ JSON /* c */ OBJECT /* d */ WITH /* e */ UNIQUE /* f */ KEYS /* g */, a /* h */ IS /* i */ NOT /* j */ DOCUMENT, a IS /* k */ NOT /* l */ NFKD /* m */ NORMALIZED;
select JSON_OBJECTAGG /* a */ ( /* b */ k /* c */ VALUE /* d */ v /* e */ ) /* f */ FILTER /* g */ ( /* h */ WHERE /* i */ x /* j */ ) /* k */ OVER /* l */ w;
select JSON_ARRAYAGG /* a */ ( /* b */ v /* c */ ORDER /* d */ BY /* e */ k /* f */ ) /* g */ FILTER /* h */ ( /* i */ WHERE /* j */ x /* k */ ) /* l */ OVER /* m */ ( /* n */ ) /* o */;
-- FILTER and OVER are only for the aggregates
select xmlconcat(a) over ();
select xmlconcat(a) filter (where x);
select xmlelement(name a) over ();
select xmlagg(a) over ();
select json_object('a' : 1) over ();
select json_object('a' : 1) filter (where x);
select json_array(1) over ();
select json_array(1) filter (where x);
select json_scalar(1) over ();
select json_serialize(x) filter (where x);
select json_query(a, 'x') over ();
select json_exists(a, 'x') filter (where x);
select json_value(a, 'x') over ();
select json('{}') over ();
select json_objectagg(k : v) over () over ();
select json_arrayagg(v) filter (where x) over w filter (where y);
select json_arrayagg(v) within group (order by v) over ();
-- the aggregates in other expressions
select 1 + json_arrayagg(v) filter (where x) from t;
select json_arrayagg(v) filter (where x) is json from t;
select json_arrayagg(v)::jsonb from t;
select (json_arrayagg(v) filter (where x)) ->> 1 from t;
select f(json_arrayagg(v) filter (where x)) from t;
select array_agg(x order by json_arrayagg(y) filter (where z)) from t;
select count(*) filter (where json_objectagg(k : v) is json) from t;
select * from f(json_arrayagg(v) filter (where x));
select * from f(json_arrayagg(v) over ());
select * from f(sum(v) over ());
select x from t group by json_arrayagg(v) having json_objectagg(k : v) is json;
select x from t order by json_arrayagg(v) over ();
select json_arrayagg(v) over (partition by json_arrayagg(w) over ()) from t;
select sum(x) over (partition by json_arrayagg(w) filter (where y)) from t;
select x from t window w as (partition by json_arrayagg(v) filter (where y));
-- special syntax in a list of arguments and as operands
select coalesce(xmlconcat(a), json_array(1)), greatest(json_scalar(1), json_value(a, 'x'));
select xmlconcat(a) || xmlconcat(b), json_array(1) -> 0, json_array(1)::text, json_array(1) is json;
select not json_exists(a, 'x'), json_exists(a, 'x') and json_exists(b, 'y');
select case when json_exists(a, 'x') then json_value(a, 'x') else json_query(a, 'y') end;
select x from t where json_exists(a, 'x') and a is json and xmlexists('//a' passing b);
select x from t order by json_value(a, 'x'), xmlserialize(document b as text);
select x from t group by json_value(a, 'x') having json_exists(a, 'x');
select (xmlelement(name a)).x;
select xmlelement(name a)::text;
select xmlelement(name a) || xmlelement(name b) is document;
select (json_array(1))[1];
select json_array(1)[1];
select json_array(1).x;
select ARRAY[json_array(1), json_object()];
select ROW(json_array(1), xmlconcat(a));
select (json_array(1), json_object());
select json_array(1) between json_array(0) and json_array(2);
select json_array(1) in (json_array(1), json_array(2));
select json_array(1) = any (array[1]);
select json_array(1) like 'x';
select json_array(1) is null, json_array(1) isnull, json_array(1) notnull;
select exists (select json_array(1));
select (select json_array(1));
select json_array(1) collate "C";
select json_array(1) at time zone 'utc';
select - json_array(1);
select json_array(1) from json_array(1);
select json_array(1) as json_array, json_object() json_object, xmlconcat(a) xmlconcat;
select json_array(1) json from t;
select json_array(1) format from t;
select json_array(1) format json from t;
select 1 format json from t;
select a format json from t;
select a format as f from t;
select a format x from t;
select format from t;
select t.format from t;
select json_array(1) as format json from t;
