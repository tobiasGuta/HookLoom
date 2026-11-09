'use strict';
const test = require('node:test');
const assert = require('node:assert/strict');
const {covers, findShadowedRules} = require('../web/rule_conflicts.js');
const r = match => ({match, response:{status:200,headers:{},body:'',delayMs:0}});
const warnings = (...rules) => findShadowedRules(rules.map(r));

test('prefix before exact shadows a more specific path', () => {
  const result = warnings({method:'GET',pathPrefix:'/callback'}, {method:'GET',pathExact:'/callback/admin'});
  assert.equal(result.length, 1);
  assert.equal(result[0].earlier, 0);
  assert.equal(result[0].later, 1);
  assert.match(result[0].detail, /callback\/admin/);
});

test('specific exact path before broad prefix is not unreachable', () => {
  assert.deepEqual(warnings({method:'GET',pathExact:'/callback/admin'}, {method:'GET',pathPrefix:'/callback'}), []);
});

test('exact before same exact shadows duplicate, but exact before prefix does not', () => {
  assert.equal(warnings({pathExact:'/x'}, {pathExact:'/x'}).length, 1);
  assert.deepEqual(warnings({pathExact:'/x'}, {pathPrefix:'/x'}), []);
  assert.deepEqual(warnings({pathExact:'/x'}, {pathExact:'/x/y'}), []);
});

test('prefix semantics include non-segment boundaries', () => {
  assert.equal(warnings({pathPrefix:'/callback'}, {pathExact:'/callbackxyz'}).length, 1);
  assert.deepEqual(warnings({pathPrefix:'/callback'}, {pathExact:'/other'}), []);
  assert.deepEqual(warnings({pathPrefix:'/callback/admin'}, {pathExact:'/callback'}), []);
});

test('unrestricted earlier method shadows a narrower method', () => {
  assert.equal(warnings({pathPrefix:'/callback'}, {method:'POST',pathExact:'/callback'}).length, 1);
  assert.deepEqual(warnings({method:'POST',pathPrefix:'/callback'}, {method:'GET',pathExact:'/callback'}), []);
  assert.deepEqual(warnings({method:'GET',pathPrefix:'/callback'}, {pathExact:'/callback'}), []);
});

test('explicit catch-all shadows later rules; not vice versa', () => {
  assert.equal(warnings({}, {method:'GET',pathExact:'/a'}).length, 1);
  assert.deepEqual(warnings({method:'GET'}, {}), []);
});

test('query constraints use exact case-sensitive key and value', () => {
  assert.equal(warnings({queryKey:'role'}, {queryKey:'role',queryValue:'admin'}).length, 1);
  assert.equal(warnings({queryKey:'role',queryValue:'admin'}, {queryKey:'role',queryValue:'admin',pathExact:'/a'}).length, 1);
  assert.deepEqual(warnings({queryKey:'role',queryValue:'admin'}, {queryKey:'role',queryValue:'guest'}), []);
  assert.deepEqual(warnings({queryKey:'role'}, {queryKey:'Role'}), []);
  assert.deepEqual(warnings({queryKey:'role',queryValue:'admin'}, {queryKey:'role'}), []);
});

test('header keys are case-insensitive while header values are not', () => {
  assert.equal(warnings({headerKey:'X-Test',headerValue:'yes'}, {headerKey:'x-test',headerValue:'yes',pathPrefix:'/'}).length, 1);
  assert.deepEqual(warnings({headerKey:'X-Test',headerValue:'yes'}, {headerKey:'x-test',headerValue:'NO'}), []);
  assert.deepEqual(warnings({headerKey:'X-Test'}, {headerKey:'Other'}), []);
});

test('a required substring can be implied by a more specific substring', () => {
  assert.equal(warnings({bodyContains:'foo'}, {bodyContains:'foobar'}).length, 1);
  assert.deepEqual(warnings({bodyContains:'foobar'}, {bodyContains:'foo'}), []);
  assert.deepEqual(warnings({bodyContains:'foo'}, {bodyContains:''}), []);
});

test('partial overlap with different constraints is not flagged', () => {
  assert.deepEqual(warnings(
    {method:'GET',pathPrefix:'/callback',queryKey:'role',queryValue:'admin'},
    {method:'GET',pathPrefix:'/callback',headerKey:'X-Admin',headerValue:'yes'}
  ), []);
});

test('warns only once per later rule, reporting the first blocking rule', () => {
  const found = warnings({}, {method:'GET'}, {method:'GET',pathExact:'/callback'});
  assert.deepEqual(found.map(w=>[w.earlier,w.later]), [[0,1],[0,2]]);
});

test('keeps indexes stable when draft conditions are invalid or incomplete', () => {
  const found = findShadowedRules([r({pathPrefix:'invalid'}),null,r({method:'GET',pathPrefix:'/callback'}),r({method:'GET',pathExact:'/callback/admin'})]);
  assert.deepEqual(found.map(w=>[w.earlier,w.later]), [[2,3]]);
});

test('unknown or mixed match shapes are ignored rather than emitting false warnings', () => {
  assert.deepEqual(warnings({pathExact:'/x',pathPrefix:'/x'}, {pathExact:'/x'}), []);
  assert.deepEqual(warnings({queryValue:'admin'}, {pathExact:'/x'}), []);
  assert.deepEqual(findShadowedRules([r({}), {match:{method:99}}]), []);
});

test('cover helper is conservative for stricter earlier rules', () => {
  assert.equal(covers({pathExact:'/callback/admin'}, {pathPrefix:'/callback'}), false);
  assert.equal(covers({pathPrefix:'/callback'}, {pathExact:'/callback/admin'}), true);
});
