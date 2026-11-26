# Proposal

## Why

Владельцы выбирают бота по кассам: у каждого свой договор, свой эквайринг и свои абоненты, которым удобно платить по-своему.
Сейчас касс пять. Нужен выбор всех касс, которые принимают боты продажи VPN.

## What Changes

- 27 новых касс, каждая одной папкой в `pay/` за общим интерфейсом, по [инструкции](../../../docs/add-gateway.md).
- Карты и СБП: Heleket, YooMoney, FreeKassa, WATA, Tribute, Robokassa, Lava, CloudPayments, PayPalych, MulenPay, Antilopay,
  SeverPay, RollyPay, AuraPay, CisPay, Kassa AI, RioPay, PayPear, Overpay, Etoplatezhi, Jupiter, TabPay, ParityPay, Donut, T-Bank.
- Крипта: Cryptomus.
- Зарубежные карты: Stripe.
- Каждая касса проходит общий набор `pay/gatewaytest`. Вебхук без подписи кассы проверяется запросом статуса к её API.
- Все кассы подключены в `cmd/worker`, выключены по умолчанию.

**Не входит:** рекуррентные списания с карты, чеки «Мой налог», Apple и Google IAP, кассы без открытого API.
Проверка на живых кассах: на тестовом сервере с ключами владельца.

## Capabilities

### New Capabilities
- `gateways`: общие требования к кассам.

### Modified Capabilities

Нет.

## Impact

- Новый код: `pay/<касса>/` на каждую кассу, строка в `cmd/worker/run.go`.
- Зависимостей не добавляется.
