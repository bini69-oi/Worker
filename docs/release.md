# Как выпускать релизы

Релиз собирает и подписывает CI, когда в репозиторий приходит тег `vX.Y.Z`. Подпись нужна, чтобы установщик и
`worker update` принимали только то, что выпустил владелец, даже если кто-то получит доступ к аккаунту GitHub или к токену CI
(решение 24 в [decisions.md](decisions.md)).

## Один раз: ключ подписи

1. На своём компьютере, не на сервере с ботом: `worker sign keygen`.
2. Закрытый ключ (строка после `RELEASE_SIGNING_KEY=`) положите в менеджер паролей и в секрет репозитория:
   Settings, Secrets and variables, Actions, New repository secret, имя `RELEASE_SIGNING_KEY`.
   Больше нигде он не нужен и не показывается. Потерян ключ: выпустите новую пару, старые установки обновятся вручную.
3. Открытый ключ (блок `BEGIN PUBLIC KEY`) сохраните в `install/release.pub` вместо заглушки.
4. Вставьте тот же блок в `install/install.sh` между строками `BEGIN RELEASE KEY` и `END RELEASE KEY`.
   Тест `go test ./install` падает, если ключи в двух местах разные.
5. Закоммитьте, `make check`.

Пока в `install/release.pub` заглушка, CI не выпускает релиз, а `worker update` и установщик отказывают.

## Каждый релиз

1. `make check` зелёный, изменения в `main`.
2. `git tag v1.2.3 && git push origin v1.2.3`.
3. В Actions пройдёт `release`: проверки, сборка `worker-linux-amd64` и `worker-linux-arm64`, `SHA256SUMS`, подпись
   `SHA256SUMS.sig`, проверка подписи ключом из репозитория, публикация.
4. Откройте релиз: четыре файла (два бинарника, суммы, подпись). Проверьте на тестовом сервере `worker update`.

Проверить релиз руками:

```sh
openssl pkeyutl -verify -pubin -inkey install/release.pub -rawin -in SHA256SUMS -sigfile SHA256SUMS.sig
sha256sum -c SHA256SUMS --ignore-missing
```

## Если ключ подписи утёк

Создайте новую пару (шаги выше), замените секрет и `install/release.pub`, выпустите релиз. Серверы со старым ключом не
примут новые релизы: на них запустите новый `install.sh` (он содержит новый ключ и обновит бинарник, база и настройки
остаются).
