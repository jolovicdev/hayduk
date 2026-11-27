import { describe, expect, it } from "vitest";
import { base64ToBytes, bytesToBase64 } from "./bytes";

describe("base64 transfer helpers", () => {
  it("round-trips binary content including NULs and high bytes", () => {
    const bytes = new Uint8Array([0, 255, 1, 128, 65, 0, 23]);
    expect(base64ToBytes(bytesToBase64(bytes))).toEqual(bytes);
  });
  it("round-trips a payload larger than one btoa chunk", () => {
    const bytes = new Uint8Array(0x8000 * 2 + 17);
    for (let i = 0; i < bytes.length; i++) bytes[i] = i % 256;
    expect(base64ToBytes(bytesToBase64(bytes))).toEqual(bytes);
  });
  it("decodes empty content", () => {
    expect(base64ToBytes("")).toEqual(new Uint8Array(0));
  });
});
