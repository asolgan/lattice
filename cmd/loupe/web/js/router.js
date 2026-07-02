// The hash router's thin DOM binding around the pure grammar in
// logic/route.js: navigate() sets the hash, startRouter() wires the single
// hashchange listener and dispatches the current route.

import { parseRoute } from "./logic/route.js";

let dispatchFn = null;

// navigate routes the console to a "#/…" target. Setting location.hash fires
// hashchange, so all rendering flows through the one dispatch path (and the
// browser history picks the change up for free). Navigating to the current
// hash re-dispatches directly (hashchange won't fire), so a re-click still
// refreshes the view.
function navigate(route) {
  if (location.hash === route) {
    if (dispatchFn) dispatchFn(parseRoute(location.hash));
    return;
  }
  location.hash = route;
}

// replaceRoute swaps the current history entry instead of pushing one — used
// for default/unknown-route fallbacks so Back doesn't loop.
function replaceRoute(route) {
  location.replace("#" + route.replace(/^#/, "").replace(/^\/?/, "/"));
}

// startRouter wires hashchange to the dispatcher and dispatches once for the
// initial URL.
function startRouter(dispatch) {
  dispatchFn = dispatch;
  window.addEventListener("hashchange", () => dispatch(parseRoute(location.hash)));
  dispatch(parseRoute(location.hash));
}

export { navigate, replaceRoute, startRouter };
