import { render } from "solid-js/web";

import "@fontsource/ibm-plex-sans/400.css";
import "@fontsource/ibm-plex-sans/500.css";
import "@fontsource/ibm-plex-sans/600.css";
import "@fontsource/ibm-plex-mono/400.css";
import "@fontsource/ibm-plex-mono/500.css";
import "@fontsource/ibm-plex-mono/600.css";
import "@phosphor-icons/web/regular";
import "@phosphor-icons/web/fill";
import "./styles/tokens.css";
import "./styles/layout.css";
import "./styles/components.css";

import App from "./app";

render(() => <App />, document.body);
