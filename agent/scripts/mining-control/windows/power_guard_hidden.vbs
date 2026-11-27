rem power_guard_hidden.vbs - enforce governor (SYSTEM, every 10min)
rem Rollback: stop+delete scheduled task SRBMinerPowerGuard, delete this file
Set shell = CreateObject("WScript.Shell")
cl = "cmd.exe /c powershell -NoProfile -ExecutionPolicy Bypass -WindowStyle Hidden -File ""D:\program\srbminer\power_governor.ps1"" -Enforce"
rc = shell.Run(cl, 0, False)
