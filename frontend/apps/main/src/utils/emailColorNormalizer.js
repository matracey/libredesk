import { modeHsl, modeLrgb, modeRgb, parse, parseNamed, useMode, wcagLuminance } from 'culori/fn'

const toRgb = useMode(modeRgb)
const toHsl = useMode(modeHsl)
useMode(modeLrgb)
const DARK_MIN_LIGHTNESS = 0.08
const DARK_MAX_LIGHTNESS = 0.92
const MIN_CONTRAST_RATIO = 4.5
const CONTRAST_ROUNDING_MARGIN = 0.05
const MAX_CACHE_ENTRIES = 512
const CONTEXTUAL_COLOR = /^(currentcolor|inherit|initial|revert(?:-layer)?|unset)$|\bvar\s*\(/i

const remappedColorCache = new Map()
const contrastColorCache = new Map()

const clamp = (value, min = 0, max = 1) => Math.min(max, Math.max(min, value))

const memoize = (compute, keyFor = (value) => value) => {
  const cache = new Map()
  return (...args) => {
    const key = keyFor(...args)
    if (cache.has(key)) return cache.get(key)
    const value = compute(...args)
    if (cache.size >= MAX_CACHE_ENTRIES) cache.delete(cache.keys().next().value)
    cache.set(key, value)
    return value
  }
}

const cacheValue = (cache, key, value) => {
  if (cache.size >= MAX_CACHE_ENTRIES) {
    cache.delete(cache.keys().next().value)
  }
  cache.set(key, value)
  return value
}

const hslToRgb = ({ h, s, l, a = 1 }) => {
  const { r, g, b } = toRgb({ mode: 'hsl', h, s, l })
  return { r, g, b, a }
}

const rgbToHsl = ({ r, g, b, a = 1 }) => {
  const hsl = toHsl({ mode: 'rgb', r, g, b })
  return { h: hsl.h ?? 0, s: hsl.s, l: hsl.l, a }
}

const parseNativeColor = (value) => {
  if (typeof document === 'undefined' || typeof window === 'undefined') return undefined
  const probe = document.createElement('span')
  probe.style.setProperty('color', value, 'important')
  if (!probe.style.color) return undefined
  probe.hidden = true
  try {
    document.documentElement.appendChild(probe)
    const computed = window.getComputedStyle(probe).color
    return /^rgba?\(/i.test(computed) ? parse(computed) : undefined
  } finally {
    probe.remove()
  }
}

const parseNormalizedColor = memoize((lower) => {
  if (CONTEXTUAL_COLOR.test(lower)) return null
  if (lower === 'transparent') {
    return Object.freeze({ r: 0, g: 0, b: 0, a: 0 })
  }
  let color = /^[a-z]+$/.test(lower)
    ? parseNamed(lower)
    : /^(#|rgba?\(|hsla?\()/.test(lower)
      ? parse(lower)
      : undefined
  color ||= parseNativeColor(lower)
  if (color?.mode === 'hsl') {
    color = { ...color, s: clamp(color.s), l: clamp(color.l) }
  }
  const rgb = color && toRgb(color)
  const channels = rgb && [rgb.r, rgb.g, rgb.b, rgb.alpha ?? 1]
  return channels?.every(Number.isFinite)
    ? Object.freeze({ r: clamp(rgb.r), g: clamp(rgb.g), b: clamp(rgb.b), a: clamp(rgb.alpha ?? 1) })
    : null
})

export const parseCssColor = (value) =>
  typeof value === 'string' ? parseNormalizedColor(value.trim().toLowerCase()) : null

const formatNumber = (value, precision = 2) => Number(value.toFixed(precision)).toString()

const formatHslColor = ({ h, s, l, a = 1 }) => {
  const channels = `${formatNumber(h)} ${formatNumber(s * 100)}% ${formatNumber(l * 100)}%`
  if (a === 1) return `hsl(${channels})`
  return `hsl(${channels} / ${formatNumber(a, 3)})`
}

const composite = (foreground, background) => {
  const alpha = foreground.a + background.a * (1 - foreground.a)
  if (alpha === 0) return { r: 0, g: 0, b: 0, a: 0 }

  return {
    r: (foreground.r * foreground.a + background.r * background.a * (1 - foreground.a)) / alpha,
    g: (foreground.g * foreground.a + background.g * background.a * (1 - foreground.a)) / alpha,
    b: (foreground.b * foreground.a + background.b * background.a * (1 - foreground.a)) / alpha,
    a: alpha
  }
}

/**
 * Blends a foreground color over a background color.
 * @param {string} foregroundValue
 * @param {string} backgroundValue
 * @returns {string | null}
 */
export const compositeCssColor = (foregroundValue, backgroundValue) => {
  const foreground = parseCssColor(foregroundValue)
  const background = parseCssColor(backgroundValue)
  if (!foreground || !background) return null
  if (foreground.a === 1) return foregroundValue
  return formatHslColor(rgbToHsl(composite(foreground, background)))
}

const relativeLuminance = (color) => wcagLuminance({ ...color, mode: 'rgb' })

const contrastRatio = (foreground, background) => {
  const opaqueBackground =
    background.a === 1 ? background : composite(background, { r: 0, g: 0, b: 0, a: 1 })
  const first = relativeLuminance(composite(foreground, opaqueBackground))
  const second = relativeLuminance(opaqueBackground)
  return (Math.max(first, second) + 0.05) / (Math.min(first, second) + 0.05)
}

export const getContrastRatio = (foregroundValue, backgroundValue) => {
  const foreground = parseCssColor(foregroundValue)
  const background = parseCssColor(backgroundValue)
  if (!foreground || !background) return null
  return contrastRatio(foreground, background)
}

/**
 * Remaps a CSS color for dark mode without changing its hue or saturation.
 * @param {string} value
 * @returns {string}
 */
export const remapColorForDarkMode = (value) => {
  if (remappedColorCache.has(value)) return remappedColorCache.get(value)

  const parsed = parseCssColor(value)
  if (!parsed || parsed.a === 0) return cacheValue(remappedColorCache, value, value)

  const hsl = rgbToHsl(parsed)
  hsl.l = DARK_MIN_LIGHTNESS + (1 - hsl.l) * (DARK_MAX_LIGHTNESS - DARK_MIN_LIGHTNESS)
  return cacheValue(remappedColorCache, value, formatHslColor(hsl))
}

const findPassingLightness = (hsl, background, direction, minimumRatio) => {
  let failing = hsl.l
  let passing = direction > 0 ? 1 : 0
  if (contrastRatio(hslToRgb({ ...hsl, l: passing }), background) < minimumRatio) {
    return null
  }

  for (let iteration = 0; iteration < 14; iteration += 1) {
    const midpoint = (failing + passing) / 2
    if (contrastRatio(hslToRgb({ ...hsl, l: midpoint }), background) >= minimumRatio) {
      passing = midpoint
    } else {
      failing = midpoint
    }
  }
  return clamp(passing + direction * 0.0001)
}

const findPassingLightnessCandidates = (hsl, background, targetRatio, preferLighter) =>
  [
    findPassingLightness(hsl, background, -1, targetRatio),
    findPassingLightness(hsl, background, 1, targetRatio)
  ]
    .filter((lightness) => lightness !== null)
    .sort((left, right) => {
      const distance = Math.abs(left - hsl.l) - Math.abs(right - hsl.l)
      if (distance !== 0 || preferLighter === undefined) return distance
      return preferLighter ? right - left : left - right
    })

/**
 * Adjusts a foreground color until it meets the requested contrast ratio.
 * @param {string} foregroundValue
 * @param {string} backgroundValue
 * @param {number} [minimumRatio=4.5]
 * @param {boolean} [preferLighter]
 * @returns {string}
 */
export const ensureColorContrast = (
  foregroundValue,
  backgroundValue,
  minimumRatio = MIN_CONTRAST_RATIO,
  preferLighter
) => {
  const cacheKey = `${foregroundValue}\0${backgroundValue}\0${minimumRatio}\0${preferLighter}`
  if (contrastColorCache.has(cacheKey)) return contrastColorCache.get(cacheKey)

  const foreground = parseCssColor(foregroundValue)
  const background = parseCssColor(backgroundValue)
  if (!foreground || foreground.a === 0 || !background) {
    return cacheValue(contrastColorCache, cacheKey, foregroundValue)
  }
  if (contrastRatio(foreground, background) >= minimumRatio) {
    return cacheValue(contrastColorCache, cacheKey, foregroundValue)
  }

  const hsl = rgbToHsl(foreground)
  const targetRatio = minimumRatio + CONTRAST_ROUNDING_MARGIN
  let searchHsl = hsl
  let candidates = findPassingLightnessCandidates(searchHsl, background, targetRatio, preferLighter)

  if (candidates.length === 0 && hsl.a !== 1) {
    // Alpha blending caps the contrast achievable by adjusting lightness alone (a near-fully
    // transparent foreground is dominated by the background regardless of its own lightness).
    // Fall back to a fully opaque foreground: black or white against any background reaches a
    // ratio of up to ~21:1, so this recovers WCAG AA (4.5:1) requests that the lightness-only
    // search above could not. It is not guaranteed to satisfy every possible `minimumRatio` -
    // e.g. some backgrounds cap opaque contrast below AAA's 7:1 - in which case the final
    // fallback below still applies.
    searchHsl = { ...hsl, a: 1 }
    candidates = findPassingLightnessCandidates(searchHsl, background, targetRatio, preferLighter)
  }

  if (candidates.length === 0) {
    return cacheValue(contrastColorCache, cacheKey, foregroundValue)
  }
  return cacheValue(
    contrastColorCache,
    cacheKey,
    formatHslColor({ ...searchHsl, l: candidates[0] })
  )
}

/**
 * Remaps a foreground and background together, keeping their contrast direction.
 * @param {string} foregroundValue
 * @param {string} backgroundValue
 * @param {string} [backdropValue='rgb(0, 0, 0)']
 * @returns {{color: string, backgroundColor: string}}
 */
export const remapColorPairForDarkMode = (
  foregroundValue,
  backgroundValue,
  backdropValue = 'rgb(0, 0, 0)'
) => {
  const foreground = parseCssColor(foregroundValue)
  const background = parseCssColor(backgroundValue)
  const remappedForeground = remapColorForDarkMode(foregroundValue)
  const remappedBackground = remapColorForDarkMode(backgroundValue)
  const contrastBackground =
    compositeCssColor(remappedBackground, backdropValue) || remappedBackground

  if (!foreground || !background) {
    return {
      color: remappedForeground,
      backgroundColor: remappedBackground
    }
  }

  return {
    color: ensureColorContrast(
      remappedForeground,
      contrastBackground,
      MIN_CONTRAST_RATIO,
      relativeLuminance(foreground) <= relativeLuminance(background)
    ),
    backgroundColor: remappedBackground
  }
}
