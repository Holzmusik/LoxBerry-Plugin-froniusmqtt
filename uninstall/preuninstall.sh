#!/bin/bash
systemctl stop froniusmqtt.service 2>/dev/null
systemctl disable froniusmqtt.service 2>/dev/null
rm -f /etc/systemd/system/froniusmqtt.service
rm -f /etc/logrotate.d/froniusmqtt

systemctl daemon-reload
exit 0
