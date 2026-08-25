// The Escape key handler for modal dialogs. An overlay sitting on top of the
// dialog (a context menu) closes first; the dialog waits for the next press.
export function escapeCloser(opts: {
  onClose: () => void;
  overlayOpen?: () => boolean;
}): (e: { key: string }) => void {
  return e => {
    if (e.key !== "Escape") return;
    if (opts.overlayOpen?.()) return;
    opts.onClose();
  };
}
