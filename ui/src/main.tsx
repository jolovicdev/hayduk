import { render } from "solid-js/web";

// latin-only subsets: the console is an english-language tool and the full
// families (cyrillic, greek, vietnamese, ...) tripled the embedded UI
import "@fontsource/ibm-plex-sans/latin-400.css";
import "@fontsource/ibm-plex-sans/latin-500.css";
import "@fontsource/ibm-plex-sans/latin-600.css";
import "@fontsource/ibm-plex-mono/latin-400.css";
import "@fontsource/ibm-plex-mono/latin-500.css";
import "@fontsource/ibm-plex-mono/latin-600.css";
// only the icons the app renders (scripts/subset-icons.sh regenerates it)
import "./styles/icons.css";
import "./styles/tokens.css";
import "./styles/layout.css";
import "./styles/components.css";

import App from "./app";

render(() => <App />, document.body);
