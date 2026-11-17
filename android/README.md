# android/ — приложение Mounvix (Kotlin + Compose)

База наследуется от проекта FileBox (GPL-3.0, тот же автор): SAF-браузер, Room (избранное/недавние), мультиселект, поиск/сортировка, темы. Ребрендинг: applicationId/namespace `com.mounvix.app`, имя «Mounvix», БД `mounvix.db`, prefs `mounvix_prefs`.

## Сборка (Windows)
```powershell
$env:JAVA_HOME = 'C:\android-dev\jdk\jdk-17.0.21+9'
& 'C:\android-dev\gradle-8.5\bin\gradle.bat' -p C:\workspace\dsh\Mounvix\android assembleDebug --no-daemon
```

## Запуск
```powershell
$adb = 'C:\android-dev\sdk\platform-tools\adb.exe'
& $adb install -r app\build\outputs\apk\debug\app-debug.apk
& $adb shell am start -n com.mounvix.app/.MainActivity
```

## Статус (M2, 2026-11-17)
- Собирается и запускается, брендинг Mounvix. Известное: каталоги исходников пока com/filebox (косметика).
- Дальше: источники WebDAV/SMB/SFTP, передачи, экран серверов, update-check; затем медиа/шеринг (M3).