@echo off
rem Run member_id_server.py with the venv 사진인식_설치.bat prepared.
rem _run_production.bat passes the exact path via MEMBERID_VENV_PY; if that is
rem not set (e.g. running this manually), fall back to the same default path.
if not defined MEMBERID_VENV_PY set "MEMBERID_VENV_PY=%ProgramData%\LuminousMemberID\venv\Scripts\python.exe"
"%MEMBERID_VENV_PY%" "%~dp0..\src\member_id_server.py"
