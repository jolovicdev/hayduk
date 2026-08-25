let flashTimer: number | undefined;

export function flash(msg: string) {
  const el = document.getElementById("statusflash");
  if (!el) return;
  el.textContent = msg;
  el.classList.add("show");
  window.clearTimeout(flashTimer);
  flashTimer = window.setTimeout(() => el.classList.remove("show"), 2600);
}
