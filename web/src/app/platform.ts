// Платформа устройства по строке браузера. Её можно сменить на экране: компьютер подключают и с телефона.
export type Platform = 'ios' | 'android' | 'desktop'

export const PLATFORMS: Platform[] = ['ios', 'android', 'desktop']

export function detectPlatform(userAgent: string): Platform {
  if (/android/i.test(userAgent)) return 'android'
  if (/iphone|ipad|ipod/i.test(userAgent)) return 'ios'
  return 'desktop'
}
