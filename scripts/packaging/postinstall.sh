#!/bin/sh
systemctl daemon-reload
systemctl enable octopus
systemctl start octopus || true
