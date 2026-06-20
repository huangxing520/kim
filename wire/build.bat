@echo off
REM 执行第一个命令
protobin\bin\protoc.exe -I proto\ --go_out=. proto\*.proto


REM 暂停以查看输出（可选）
pause
