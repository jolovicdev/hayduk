export type PanelSizes = { left: number; right: number; nb: number };

export const DEFAULT_PANELS: PanelSizes = { left: 274, right: 314, nb: 214 };

const LEFT_LIMITS = [200, 560] as const;
const RIGHT_LIMITS = [250, 640] as const;
export const NOTEBOOK_MIN = 140;

function clamp(v: number, min: number, max: number): number {
  return Math.min(Math.max(v, min), max);
}

export { clamp };

// The notebook max tracks the viewport, but a short viewport must never push
// the maximum below the minimum - that collapsed the notebook to nothing.
export function clampNotebook(v: number, viewportHeight: number): number {
  return clamp(v, NOTEBOOK_MIN, Math.max(NOTEBOOK_MIN, viewportHeight - 300));
}

export function clampPanels(p: Partial<PanelSizes>, viewportHeight: number): PanelSizes {
  return {
    left: Number.isFinite(p.left) ? clamp(p.left as number, LEFT_LIMITS[0], LEFT_LIMITS[1]) : DEFAULT_PANELS.left,
    right: Number.isFinite(p.right) ? clamp(p.right as number, RIGHT_LIMITS[0], RIGHT_LIMITS[1]) : DEFAULT_PANELS.right,
    nb: Number.isFinite(p.nb) ? clampNotebook(p.nb as number, viewportHeight) : DEFAULT_PANELS.nb,
  };
}
