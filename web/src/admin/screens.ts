import type { Component } from 'svelte'
import Payments from './Payments.svelte'
import Users from './Users.svelte'

type Screen = Component<{ arg?: string }>

// Экран без вложенных адресов не берёт arg, но App отдаёт его всем экранам одинаково.
const plain = (c: unknown) => c as Screen

// Экран раздела по его номеру в меню. arg это вторая часть адреса: #/users/<arg>.
export const screens: Record<string, Screen> = {
  users: Users,
  payments: plain(Payments),
}
