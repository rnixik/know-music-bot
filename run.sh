#!/bin/bash
pkill -f know-music
nohup ./know-music > nohup.log &
