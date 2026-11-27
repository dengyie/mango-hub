rem power_governor_hidden.vbs - launch governor daemon hidden, no focus steal
rem Rollback: stop+delete scheduled task SRBMinerPowerGovernor, delete this file
Set shell = CreateObject("WScript.Shell")
cl = "cmd.exe /c powershell -NoProfile -ExecutionPolicy Bypass -WindowStyle Hidden -File ""D:\program\srbminer\power_governor.ps1"""
rc = shell.Run(cl, 0, False)
