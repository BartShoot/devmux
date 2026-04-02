#!/bin/sh
echo "/usr/lib/devmux" > /etc/ld.so.conf.d/devmux.conf
ldconfig
