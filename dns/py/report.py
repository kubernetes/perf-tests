#!/usr/bin/env python

# Copyright 2016 The Kubernetes Authors.
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

from __future__ import print_function

import argparse
import sys
import sqlite3
from glob import iglob
from terminaltables import AsciiTable


def parse_args():
  parser = argparse.ArgumentParser(
      description="""
      Ingest the output of *.out file(s) and insert it into the database.
      """,
      formatter_class=argparse.ArgumentDefaultsHelpFormatter
  )

  parser.add_argument(
      '--db',
      type=str,
      required=True,
      help='Result database for analysis'
  )

  parser.add_argument(
      '-v',
      '--verbose',
      action='store_true'
  )

  return parser.parse_args()

def go(args):
  db = sqlite3.connect(args.db, detect_types=sqlite3.PARSE_COLNAMES)

  for sqlfile in iglob("report_queries/*.sql"):
    cursor = db.execute(open(sqlfile, "r").read())
    data = cursor.fetchall()

    table_data = []
    table_data.append([c[0] for c in cursor.description if c])
    table_data = table_data + data

    table = AsciiTable(table_data)
    print("File: ", sqlfile)
    print(table.table)
    print("\n")

if __name__ == '__main__':
  sys.exit(go(parse_args()))
