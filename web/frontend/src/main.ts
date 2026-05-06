import "./styles.css";
import "./app";

declare global {
  interface Window {
    __shortyFrontendBooted?: boolean;
    BASE_URL: string;
  }
}

if (!window.__shortyFrontendBooted) {
  window.__shortyFrontendBooted = true;
}
