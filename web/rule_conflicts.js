/* HookLoom rule-conflict analyzer. Pure, dependency-free, no captured content. */
(function (root, makeAnalyzer) {
  const analyzer = makeAnalyzer();
  if (typeof module === 'object' && module.exports) module.exports = analyzer;
  if (typeof window !== 'undefined') window.HookLoomConflictAnalyzer = analyzer;
})(this, function () {
  'use strict';

  // Mirror the known Go match fields. Invalid/incomplete matches are omitted
  // from the advisory rather than creating misleading warnings while editing.
  const fields = ['method', 'pathPrefix', 'pathExact', 'queryKey', 'queryValue',
    'headerKey', 'headerValue', 'bodyContains'];
  const methodRE = /^[!#$%&'*+.^_`|~0-9A-Za-z-]+$/;

  function normalize(input) {
    if (!input || typeof input !== 'object' || Array.isArray(input)) return null;
    const result = {};
    for (const key of fields) {
      const value = input[key] === undefined ? '' : input[key];
      if (typeof value !== 'string') return null;
      result[key] = value;
    }
    if (result.method && (!methodRE.test(result.method) || result.method !== result.method.toUpperCase())) return null;
    if ((result.pathPrefix && !result.pathPrefix.startsWith('/')) ||
        (result.pathExact && !result.pathExact.startsWith('/')) ||
        (result.pathPrefix && result.pathExact) ||
        (result.queryValue && !result.queryKey) ||
        (result.headerValue && !result.headerKey) ||
        (result.headerKey && !methodRE.test(result.headerKey))) return null;
    return result;
  }

  function sameOrUnrestricted(earlier, later) {
    return !earlier || earlier === later;
  }

  // A simple single key/value condition in an earlier rule is covered only
  // if the later rule requires that same key and at least the same value.
  function coversKeyValue(aKey, aVal, bKey, bVal, caseInsensitiveKey = false) {
    if (!aKey) return true;
    if (!bKey) return false;
    const left = caseInsensitiveKey ? aKey.toLowerCase() : aKey;
    const right = caseInsensitiveKey ? bKey.toLowerCase() : bKey;
    return left === right && (!aVal || aVal === bVal);
  }

  function coversPath(a, b) {
    if (a.pathExact) return Boolean(b.pathExact) && a.pathExact === b.pathExact;
    if (a.pathPrefix) {
      if (b.pathExact) return b.pathExact.startsWith(a.pathPrefix);
      if (b.pathPrefix) return b.pathPrefix.startsWith(a.pathPrefix);
      return false;
    }
    return true;
  }

  // Returns true only when the earlier rule is guaranteed to match every
  // request matched by the later rule. Partial overlaps are NOT flagged.
  function covers(earlier, later) {
    const a = normalize(earlier), b = normalize(later);
    if (!a || !b) return false;
    return sameOrUnrestricted(a.method, b.method) &&
      coversPath(a, b) &&
      coversKeyValue(a.queryKey, a.queryValue, b.queryKey, b.queryValue) &&
      coversKeyValue(a.headerKey, a.headerValue, b.headerKey, b.headerValue, true) &&
      (!a.bodyContains || b.bodyContains.includes(a.bodyContains));
  }

  function explanation(earlier, later) {
    if (!earlier) return '';
    if (!Object.values(earlier).some(v => v)) return 'The earlier rule is a catch-all.';
    if (earlier.pathPrefix && later?.pathExact && later.pathExact.startsWith(earlier.pathPrefix)) {
      return `The earlier “starts with ${earlier.pathPrefix}” condition also matches the exact path ${later.pathExact}.`;
    }
    if (earlier.pathPrefix && later?.pathPrefix && later.pathPrefix.startsWith(earlier.pathPrefix)) {
      return `The earlier path prefix ${earlier.pathPrefix} also matches paths beginning with ${later.pathPrefix}.`;
    }
    if (earlier.pathExact && later?.pathExact && earlier.pathExact === later.pathExact) {
      return `Both rules match the exact path ${earlier.pathExact}.`;
    }
    return 'Every condition of the earlier rule is also required by the later rule.';
  }

  function findShadowedRules(rules) {
    if (!Array.isArray(rules)) return [];
    const normalized = rules.map(rule => normalize(rule?.match));
    const warnings = [];
    for (let later = 1; later < normalized.length; later++) {
      const candidate = normalized[later];
      if (!candidate) continue;
      for (let earlier = 0; earlier < later; earlier++) {
        const prior = normalized[earlier];
        if (!prior || !covers(prior, candidate)) continue;
        warnings.push({
          earlier, later,
          title: `Rule ${later + 1} is unreachable: Rule ${earlier + 1} matches first.`,
          detail: explanation(prior, candidate),
          action: `Move Rule ${later + 1} above Rule ${earlier + 1}, or narrow Rule ${earlier + 1}.`
        });
        break; // Report the first earlier rule responsible for the shadow.
      }
    }
    return warnings;
  }

  return {covers, findShadowedRules};
});
