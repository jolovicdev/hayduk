import { describe, expect, it } from "vitest";
import { switchWorkspaceAction } from "./appActions";

describe("switchWorkspaceAction", () => {
  it("drops the host selection after a successful switch", async () => {
    const events: string[] = [];
    const sw = switchWorkspaceAction({
      command: async () => [],
      onSwitched: () => events.push("selection-dropped"),
      flash: m => events.push(m),
    });
    await sw("lab");
    expect(events).toEqual(["selection-dropped", "switched to workspace lab"]);
  });

  it("keeps the selection and flashes when the switch fails", async () => {
    const events: string[] = [];
    const sw = switchWorkspaceAction({
      command: async () => { throw new Error("no workspace"); },
      onSwitched: () => events.push("selection-dropped"),
      flash: m => events.push(m),
    });
    await sw("gone");
    expect(events).toEqual(["no workspace"]);
  });
});
