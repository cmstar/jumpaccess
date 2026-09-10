export interface TerminalBackground {
  enabled: boolean
  filePath: string
  transparencyPercent: number
  fitMode: 'cover' | 'contain' | 'stretch' | 'tile'
  positionXPercent: number
  positionYPercent: number
  tileFitLongEdge: boolean
  tileOnlyWholeTiles: boolean
}

export const defaultTerminalBackground: TerminalBackground = {
  enabled: false, filePath: '', transparencyPercent: 70, fitMode: 'cover',
  positionXPercent: 50, positionYPercent: 50, tileFitLongEdge: false, tileOnlyWholeTiles: false,
}

export function backgroundLayout(imageWidth: number, imageHeight: number, areaWidth: number, areaHeight: number, fitLongEdge: boolean, wholeTiles: boolean) {
  if (![imageWidth, imageHeight, areaWidth, areaHeight].every(value => Number.isFinite(value) && value > 0)) return undefined
  let scale = 1
  if (fitLongEdge) scale = imageWidth > imageHeight ? areaWidth / imageWidth
    : imageHeight > imageWidth ? areaHeight / imageHeight : Math.min(areaWidth, areaHeight) / imageWidth
  let tileWidth = imageWidth * scale
  let tileHeight = imageHeight * scale
  let width = areaWidth
  let height = areaHeight
  if (wholeTiles) {
    width = Math.floor(areaWidth / tileWidth + 1e-9) * tileWidth
    height = Math.floor(areaHeight / tileHeight + 1e-9) * tileHeight
    if (width === 0 || height === 0) {
      scale = Math.min(areaWidth / imageWidth, areaHeight / imageHeight)
      width = tileWidth = imageWidth * scale
      height = tileHeight = imageHeight * scale
    }
  }
  return { tileWidth, tileHeight, width, height }
}
