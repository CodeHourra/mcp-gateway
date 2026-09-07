declare module '*wails/runtime.js' {
  export const Events: { On(name: string, callback: (event: { data: unknown }) => void): () => void }
}
