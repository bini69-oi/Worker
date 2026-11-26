# Кассы

Все кассы выключены по умолчанию. Чтобы включить: заполните ключи кассы в админке, включите `<касса>.enabled`,
в кабинете кассы укажите адрес уведомлений `https://<ваш адрес>/pay/<касса>`.

«Проверена вживую» значит, что настоящий платёж прошёл на тестовом сервере. Остальные кассы проверены
на записанных примерах ответов и уведомлений: протокол восстановлен по документации и открытым реализациям.

| Касса | Что принимает | Ключи | Уведомления | Проверена вживую |
|---|---|---|---|---|
| Ручной перевод | перевод по реквизитам, админ подтверждает | `manual.details` | нет | нет |
| Telegram Stars | звёзды, автоподписка | `stars.rate` | через бота | нет |
| ЮKassa | карты, СБП | `yookassa.shop_id`, `yookassa.secret_key` | только с адресов ЮKassa, сверка с API | нет |
| CryptoBot | крипта в Telegram | `cryptobot.token` | подпись HMAC | нет |
| Platega | СБП, карты | `platega.merchant_id`, `platega.secret` | сверка с API | нет |
| Heleket | крипта, счёт в рублях | `heleket.merchant`, `heleket.api_key` | подпись md5 в теле | нет |
| ЮMoney | кошелёк ЮMoney, карты | `yoomoney.wallet`, `yoomoney.notification_secret`, `yoomoney.token` | подпись sha1 | нет |
| FreeKassa | карты, СБП, кошельки | `freekassa.shop_id`, `freekassa.secret1`, `freekassa.secret2`, `freekassa.api_key` | подпись md5, только с адресов FreeKassa | нет |
| WATA | карты, СБП | `wata.token` | подпись RSA | нет |
| Cryptomus | крипта, счёт в рублях | `cryptomus.merchant_id`, `cryptomus.api_key` | подпись md5 в теле | нет |
| Robokassa | карты, СБП | `robokassa.login`, `robokassa.password1`, `robokassa.password2`, `robokassa.hash` | подпись по паролю 2, ответ `OK<номер>` | нет |
| Lava | карты, СБП | `lava.shop_id`, `lava.secret_key`, `lava.hook_key` | подпись HMAC-SHA256 | нет |
| CloudPayments | карты | `cloudpayments.public_id`, `cloudpayments.api_secret` | подпись HMAC-SHA256, ответ `{"code":0}`, адрес Pay, Fail, Refund в кабинете | нет |
| PayPalych | карты, СБП | `paypalych.token`, `paypalych.shop_id` | подпись md5, Result URL в кабинете | нет |
| Tribute | донат через Telegram | `tribute.api_key`, `tribute.link` | подпись HMAC-SHA256; сумму абонент вводит сам, платёж находится по Telegram ID и сумме | нет |
| MulenPay | карты, СБП | `mulenpay.api_key`, `mulenpay.shop_id`, `mulenpay.secret_key` | подпись sha1 в теле, сумма и статус только из API | нет |
| Antilopay | СБП, карты, SberPay | `antilopay.project_id`, `antilopay.secret_id`, `antilopay.private_key`, `antilopay.public_key` | подпись RSA | нет |
| SeverPay | карты, СБП | `severpay.mid`, `severpay.token` | подпись HMAC-SHA256, ответ `{"status":true}` | нет |
| RollyPay | СБП, карты | `rollypay.api_key`, `rollypay.signing_secret` | подпись HMAC-SHA256, оплата подтверждается API | нет |
| AuraPay | СБП, карты | `aurapay.api_key`, `aurapay.shop_id`, `aurapay.secret_key` | подпись HMAC-SHA256 по полям | нет |
| CisPay | карты, СБП | `cispay.shop_id`, `cispay.api_key` | подпись HMAC-SHA256, оплата подтверждается API | нет |
| Kassa AI | СБП, карты, SberPay | `kassaai.shop_id`, `kassaai.api_key`, `kassaai.secret2`, `kassaai.server_ip` | подпись md5, ответ `YES` | нет |
| RioPay | карты, СБП | `riopay.api_token`, `riopay.webhook_secret` | подпись HMAC-SHA512, оплата подтверждается API | нет |
| PayPear | СБП, карты, SberPay, T-Pay | `paypear.shop_id`, `paypear.secret_key` | подписи нет, статус и сумма только из API | нет |
| Overpay | карты, СБП | `overpay.username`, `overpay.password`, `overpay.project_id`, сертификат по желанию | подписи нет, статус и сумма только из API, при недоступном API отказ | нет |
