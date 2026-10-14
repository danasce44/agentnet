// Execute Comic's source model with its shipped, bundled Markdown projection.
// Node does not resolve the bundler's extensionless TypeScript imports, and
// these Go/Node fixtures do not require an npm installation.
import { registerHooks } from 'node:module';
const modelURL = new URL('../web/src/model.ts', import.meta.url).href;
registerHooks({ resolve(specifier, context, nextResolve) {
  if (context.parentURL === modelURL && specifier === './linkText')
    return nextResolve(new URL('../skins/shared/link-text.mjs', import.meta.url).href, context);
  return nextResolve(specifier, context);
} });
export default await import(modelURL);
