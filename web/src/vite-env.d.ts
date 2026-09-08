/// <reference types="vite/client" />

interface ImportMetaEnv {
  // "wasm" (default) runs the chain in the page; "http" talks to a dyld
  // server on the same origin, which is what `npm run dev` does.
  readonly VITE_BACKEND?: 'wasm' | 'http'
}

interface ImportMeta {
  readonly env: ImportMetaEnv
}
