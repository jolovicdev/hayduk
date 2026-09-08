import { flash } from "./statusflash";

// copyText works on the documented plain-HTTP team setup, where
// navigator.clipboard is undefined (the async clipboard API needs a secure
// context): fall back to a selected textarea and the legacy copy command
// instead of throwing.
export async function copyText(text: string): Promise<boolean> {
  if (navigator.clipboard && window.isSecureContext) {
    try {
      await navigator.clipboard.writeText(text);
      return true;
    } catch {
      // permission denied or document unfocused: try the legacy path
    }
  }
  try {
    const ta = document.createElement("textarea");
    ta.value = text;
    ta.setAttribute("readonly", "");
    ta.style.position = "fixed";
    ta.style.top = "0";
    ta.style.opacity = "0";
    document.body.appendChild(ta);
    ta.focus();
    ta.select();
    const ok = document.execCommand("copy");
    ta.remove();
    return ok;
  } catch {
    return false;
  }
}

// copyWithFeedback is the context-menu flavor: every copy action reports
// whether it actually reached the clipboard.
export function copyWithFeedback(text: string) {
  void copyText(text).then(ok => flash(ok ? "Copied" : "copy unavailable in this browser context"));
}
