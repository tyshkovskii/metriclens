export function isEditable(target: EventTarget | null) {
  if (!(target instanceof HTMLElement)) {
    return false;
  }
  const tag = target.tagName;
  if (target instanceof HTMLInputElement) {
    // The scrubber handles its own arrow keys; treat other inputs as editable.
    return target.type !== "range";
  }
  return tag === "TEXTAREA" || tag === "SELECT" || target.isContentEditable;
}
