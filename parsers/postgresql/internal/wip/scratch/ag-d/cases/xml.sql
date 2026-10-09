-- XMLCONCAT
select xmlconcat(a);
select xmlconcat(a, b, c);
SELECT XMLCONCAT('<a/>', '<b/>');
select xmlconcat /* c */ ( /* c */ a /* c */ , /* c */ b /* c */ ) -- c
;
-- XMLELEMENT, the four forms
select xmlelement(name foo);
select xmlelement(name foo, xmlattributes(a));
select xmlelement(name foo, xmlattributes(a as b));
select xmlelement(name foo, xmlattributes(a as b, c, d as "E"));
select xmlelement(name foo, a, b);
select xmlelement(name foo, xmlattributes(a as b), c, d);
select xmlelement(name "Foo", xmlattributes(1 as x), 'text');
select xmlelement(name foo, xmlelement(name bar, xmlattributes(now() as t)));
select XMLELEMENT(NAME select);
select xmlelement(name xmlattributes);
select xmlelement(name foo, xmlattributes(a || b as c) , x + 1);
select xmlelement(name foo, bar);
-- XMLEXISTS
select xmlexists('//a' passing x);
select xmlexists('//a' passing by ref x);
select xmlexists('//a' passing by value x);
select xmlexists('//a' passing x by ref);
select xmlexists('//a' passing x by value);
select xmlexists('//a' passing by ref x by ref);
select xmlexists('//a' passing by value x by value);
select xmlexists(path passing by ref doc);
select xmlexists('//a' passing by);
select xmlexists('//a' passing by by ref);
select xmlexists(f(x) passing x);
select xmlexists('//a' passing (x));
-- XMLFOREST
select xmlforest(a);
select xmlforest(a as b);
select xmlforest(a as b, c, 1 + 2 as d, 'x');
-- XMLPARSE
select xmlparse(document x);
select xmlparse(content x);
select xmlparse(document x preserve whitespace);
select xmlparse(document x strip whitespace);
select xmlparse(content '<a/>' preserve whitespace);
select xmlparse(content '<a/>' strip whitespace);
select xmlparse(document 'a' || 'b');
-- XMLPI
select xmlpi(name php);
select xmlpi(name php, 'echo "hello world";');
select xmlpi(name "xml", x || y);
-- XMLROOT
select xmlroot(x, version '1.0');
select xmlroot(x, version no value);
select xmlroot(x, version '1.0', standalone yes);
select xmlroot(x, version '1.0', standalone no);
select xmlroot(x, version '1.0', standalone no value);
select xmlroot(x, version no value, standalone yes);
select xmlroot(x, version no value, standalone no);
select xmlroot(x, version no value, standalone no value);
select xmlroot(x, version no);
select xmlroot(x, version 1 + 2, standalone yes);
select xmlroot(xmlelement(name a), version '1.0');
-- XMLSERIALIZE
select xmlserialize(document x as text);
select xmlserialize(content x as text);
select xmlserialize(document x as varchar(10));
select xmlserialize(content x as character varying);
select xmlserialize(document x as text indent);
select xmlserialize(document x as text no indent);
select xmlserialize(content x as public.mytype indent);
select xmlserialize(content x as timestamp with time zone no indent);
select xmlserialize(document x as text[]);
select xmlserialize(document x as setof text);
-- IS DOCUMENT
select a is document;
select a is not document;
select a is document, b is not document;
select a is document is null;
select a is not document is not true;
select (a is document) = (b is document);
select a between b is document and c;
select a between b is not document and c is document;
select a not between b is document and c;
select a like b is document;
select 1 where x is document and y is not document or z is document;
select not a is document;
select a || b is document;
select a is document is document;
-- inside b_expr
select a between 1 and b is document;
select x from t where a in (select 1) is document;
select position(a is document in b);
