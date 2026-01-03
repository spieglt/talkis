#!/bin/bash

amixer sset 'Master' 90%
amixer sset 'Capture' 90%
systemctl --user stop talkis
go build -o talkis .
sudo cp ./talkis /usr/local/bin
sudo cp ./talkis.service /etc/systemd/user/
systemctl --user enable talkis
systemctl --user start talkis
loginctl enable-linger $USER
