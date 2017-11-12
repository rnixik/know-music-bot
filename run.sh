#!/bin/bash
pkill -f guess-song
nohup ./guess-song > nohup.log &
