import base from './playwright.config';

/** Run against an already-up desktop node (e.g. http://127.0.0.1:8080). No e2e_stack webServer. */
export default {
  ...base,
  webServer: undefined,
};
