@echo off
cd /d C:\multica-deploy\multica\apps\web
C:\multica-deploy\multica\node_modules\.bin\dotenvx.cmd run -f ..\..\.env -- node node_modules\next\dist\bin\next start -p 3000 > C:\multica-deploy\multica\next-start.log 2>&1
