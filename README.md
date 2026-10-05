# asm

CLI-менеджер AIR SDK, версия `1.0.0`. Текущий Windows-прототип состоит из
`asm.bat` для команд и `sdk.ps1` для чтения настроек и описаний SDK.
Используется встроенный Windows PowerShell.

Из папки проекта в PowerShell:

```powershell
.\asm.bat help
.\asm.bat --version
.\asm.bat list
.\asm.bat search
.\asm.bat search 51.4
```

В cmd можно писать `asm help`, `asm --version` и `asm list`. Вызов без аргументов
показывает справку. Также работают `-h`, `--help`, `-v`, `ls`, `help list` и `list --help`.
`--version` и `-v` выводят только `1.0.0`.

`list` читает `%USERPROFILE%\.airsdk\airsdkmanager.cfg` и берёт каталог из `AIR_SDKS`:

```ini
AIR_SDKS=C:\AIRSDK
```

SDK ищутся в непосредственных подпапках этого каталога, как в AIR SDK Manager.
Версия берётся из `air-sdk-description.xml`: например, `<version>51.3.4</version>`
и `<build>1</build>` дают `51.3.4.1`. Список сортируется по числовым компонентам версии.

Пример вывода:

```text
50.2.5.1       C:\AIRSDK\AIRSDK_50.2.5
51.3.3.2       C:\AIRSDK\AIRSDK_51.3.3
51.3.4.1       C:\AIRSDK\AIRSDK_51.3.4
```

Если настройки, значение `AIR_SDKS` или указанный каталог отсутствуют, команда
сообщает об ошибке в stderr и возвращает `1`. Пустой каталог даёт сообщение об
отсутствии SDK и код `0`. Повреждённые описания SDK пропускаются с предупреждением.
Чтение списка не изменяет конфиг AIR SDK Manager.

`search` получает каталог стабильных версий через API AIR SDK Manager:

```powershell
.\asm.bat search
.\asm.bat search 51.4
.\asm.bat search 51.4.1.1
```

Выводятся полные номера SDK, от новых к старым. Фильтр сравнивает компоненты:
`51.4` включает сборки этой ветки и не совпадает с `51.40`.
Для справки работают `help search` и `search --help`.

Если API недоступен или возвращает ошибку в JSON, используется каталог из
`%USERPROFILE%\.airsdk\airsdkmanager.db` либо свежей резервной копии этой базы.
Сообщение в stderr указывает источник кеша; такой результат может быть устаревшим.
Если доступного каталога нет, команда возвращает ошибку. Поле `API_ENDPOINT`
из настроек менеджера задаёт адрес API, по умолчанию — `https://api.airsdk.harman.com`.

План остальных команд и кроссплатформенной версии — в [plan.md](plan.md).
