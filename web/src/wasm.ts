// Loads cmd/dylwasm and starts the in-page cluster. Everything the UI
// needs is put on the window as dylState / dylAccount / dylSubmitTx /
// dylFaucet / dylFault / dylProof, and events arrive through __dylEvent.

declare global {
  interface Window {
    Go: new () => {
      importObject: WebAssembly.Imports
      run: (i: WebAssembly.Instance) => Promise<void>
    }
    dylStart: () => void
    dylState: () => string
    dylAccount: (addr: string) => string
    dylProof: (height: number, hash: string) => string | { error: string }
    dylSubmitTx: (json: string) => { ok?: boolean; error?: string }
    dylFaucet: (addr: string) => { ok?: boolean; error?: string }
    dylFault: (index: number) => { ok?: boolean; error?: string }
    __dylReady: () => void
    __dylEvent: (kind: string, json: string) => void
  }
}

export type WasmEvent = { kind: string; data: unknown }

let started: Promise<void> | null = null

// loadChain fetches and instantiates the wasm module once, wires the event
// callback, and resolves when the Go side has called __dylReady. Calling it
// again returns the same promise.
export function loadChain(onEvent: (kind: string, data: unknown) => void): Promise<void> {
  window.__dylEvent = (kind, json) => {
    try {
      onEvent(kind, JSON.parse(json))
    } catch {
      /* ignore a malformed frame */
    }
  }
  if (started) return started

  started = new Promise<void>((resolve, reject) => {
    if (typeof window.Go !== 'function') {
      reject(new Error('wasm_exec.js did not load'))
      return
    }
    window.__dylReady = () => {
      window.dylStart()
      resolve()
    }
    const go = new window.Go()
    WebAssembly.instantiateStreaming(fetch('/dyl.wasm'), go.importObject)
      .then((res) => {
        // go.run never resolves (main() ends in select{}); do not await it.
        void go.run(res.instance)
      })
      .catch(reject)
  })
  return started
}
