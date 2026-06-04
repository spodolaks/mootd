// Native stub: html-to-image is a browser-only library (uses DOM APIs).
// Native builds never reach this path because OutfitCard branches on
// Platform.OS, but we still provide a matching export signature so TypeScript
// and Metro both resolve without pulling the web package into the native
// bundle.
//
// Signature matches html-to-image's toPng so call sites can stay identical.
export const toPng = async (_node: unknown, _options?: unknown): Promise<string> => {
  throw new Error('html-to-image is only available on the web bundle');
};
