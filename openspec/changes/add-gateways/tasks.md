# Tasks

## 1. Волна 1: самые частые
- [x] 1.1 Heleket; проверка: `go test ./pay/heleket`
- [x] 1.2 YooMoney; проверка: `go test ./pay/yoomoney`
- [x] 1.3 FreeKassa; проверка: `go test ./pay/freekassa`
- [x] 1.4 WATA; проверка: `go test ./pay/wata`
- [x] 1.5 Cryptomus; проверка: `go test ./pay/cryptomus`

## 2. Волна 2
- [x] 2.1 Tribute; проверка: `go test ./pay/tribute`
- [x] 2.2 Robokassa; проверка: `go test ./pay/robokassa`
- [x] 2.3 Lava; проверка: `go test ./pay/lava`
- [x] 2.4 CloudPayments; проверка: `go test ./pay/cloudpayments`
- [x] 2.5 PayPalych; проверка: `go test ./pay/paypalych`

## 3. Волна 3
- [x] 3.1 MulenPay; проверка: `go test ./pay/mulenpay`
- [x] 3.2 Antilopay; проверка: `go test ./pay/antilopay`
- [x] 3.3 SeverPay; проверка: `go test ./pay/severpay`
- [x] 3.4 RollyPay; проверка: `go test ./pay/rollypay`
- [x] 3.5 AuraPay; проверка: `go test ./pay/aurapay`

## 4. Волна 4
- [x] 4.1 CisPay; проверка: `go test ./pay/cispay`
- [x] 4.2 Kassa AI; проверка: `go test ./pay/kassaai`
- [x] 4.3 RioPay; проверка: `go test ./pay/riopay`
- [x] 4.4 PayPear; проверка: `go test ./pay/paypear`
- [x] 4.5 Overpay; проверка: `go test ./pay/overpay`

## 5. Волна 5
- [x] 5.1 Etoplatezhi; проверка: `go test ./pay/etoplatezhi`
- [x] 5.2 Jupiter; проверка: `go test ./pay/jupiter`
- [x] 5.3 TabPay; проверка: `go test ./pay/tabpay`
- [x] 5.4 ParityPay; проверка: `go test ./pay/paritypay`
- [x] 5.5 Donut; проверка: `go test ./pay/donut`

## 6. Волна 6: по документации
- [x] 6.1 T-Bank; проверка: `go test ./pay/tbank`
- [x] 6.2 Stripe; проверка: `go test ./pay/stripe`

## 7. Подключение и ревью
- [x] 7.1 Все кассы в `cmd/worker`; проверка: тест запуска видит их адреса вебхуков
- [x] 7.2 `docs/gateways.md`: касса, ключи, адрес вебхука, проверена ли вживую; проверка: ссылка из README
- [ ] 7.3 Ревью безопасности всех касс одним проверяющим; проверка: находки закрыты
- [ ] 7.4 Живая проверка на тестовом сервере с ключами владельца; проверка: отметки в `docs/gateways.md`
