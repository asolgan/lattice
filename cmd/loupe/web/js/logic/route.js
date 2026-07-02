// Pure route grammar for the hash router. No DOM, no fetch — goja-tested via
// cmd/loupe/web_logic_test.go (strip-export load). Keys contain dots but never
// "/" or "#", so a raw Core KV key is a safe path segment.

// parseRoute parses a location hash "#/view/arg?k=v&…" into
// { view, arg, params }. The arg is everything between the view's trailing
// slash and the "?" (a full entity key, dots included). Missing pieces come
// back as "" / {} — validity against the route table is the caller's job.
function parseRoute(hash) {
  var h = hash || "";
  if (h.charAt(0) === "#") h = h.slice(1);
  var params = {};
  var qi = h.indexOf("?");
  if (qi >= 0) {
    var pairs = h.slice(qi + 1).split("&");
    for (var i = 0; i < pairs.length; i++) {
      if (!pairs[i]) continue;
      var eq = pairs[i].indexOf("=");
      var k = eq >= 0 ? pairs[i].slice(0, eq) : pairs[i];
      var v = eq >= 0 ? pairs[i].slice(eq + 1) : "";
      try {
        params[decodeURIComponent(k)] = decodeURIComponent(v);
      } catch (e) {
        params[k] = v; // malformed escape — keep the raw text
      }
    }
    h = h.slice(0, qi);
  }
  if (h.charAt(0) === "/") h = h.slice(1);
  var si = h.indexOf("/");
  var view = si >= 0 ? h.slice(0, si) : h;
  var arg = si >= 0 ? h.slice(si + 1) : "";
  return { view: view, arg: arg, params: params };
}

export { parseRoute };
